// Kept identical in platform/ios (the iOS host) and platform/macos (the
// Mac's Swift library, which host/darwin calls through purego).
#if ASUKA_PURCHASE // an app that calls asuka/sdk/purchase (see the CLI's iosFeatures; on the Mac, its featurePermissions)
import Foundation
import StoreKit
#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

/// asuka/sdk/purchase's module, over StoreKit 2: internal/purchasespec's
/// methods and JSON. It listens to Transaction.updates (and gives the
/// unfinished transactions) from the app's start, keeping each for the
/// "next" that asks, so a purchase completed while Go doesn't observe
/// (Ask to Buy approved, a renewal, a refund, an offer code redeemed in
/// the App Store) isn't lost; Storefront.updates too. The Mac's needs
/// macOS 12 (its library's minimum is 11: before 12, no store).
@available(macOS 12.0, *)
@MainActor final class AsukaStore {
    static let shared = AsukaStore()

    /// The methods, by name (each answers through call).
    static let methods = ["available", "products", "buy", "finish", "entitlements", "restore", "status",
                          "manage", "refund", "redeem", "storefront", "next", "stop"]

    #if canImport(UIKit)
    /// The scene the sheets show in: the app's.
    var scene: () -> UIWindowScene? = {
        UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first { $0.activationState == .foregroundActive }
            ?? UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
    }
    #else
    /// The window the sheets show on: the app's key window.
    var window: () -> NSWindow? = { NSApp.keyWindow ?? NSApp.mainWindow ?? NSApp.windows.first { $0.isVisible } }
    #endif

    /// Products asked for, by ID: purchase needs StoreKit's.
    private var products: [String: Product] = [:]
    /// Transactions not yet finished, by ID.
    private var unfinished: [UInt64: Transaction] = [:]
    /// Transactions given, by ID and revocation: StoreKit gives an
    /// unfinished one both in Transaction.unfinished and updates.
    private var given = Set<String>()
    private var events: [Data] = []
    private var waiting: ((Result<Data, Error>) -> Void)?
    private var started = false

    struct Failure: LocalizedError {
        let message: String
        init(_ m: String) { message = m }
        var errorDescription: String? { message }
    }

    /// Starts listening: at the app's launch (Apple's advice: an update
    /// that comes before is given once the listener starts).
    func start() {
        guard !started else { return }
        started = true
        #if targetEnvironment(simulator)
        AsukaStoreKitTest.start()
        #endif
        Task { @MainActor in
            for await v in Transaction.updates { self.updated(v) }
        }
        Task { @MainActor in
            for await v in Transaction.unfinished { self.updated(v) }
        }
        Task { @MainActor in
            for await s in Storefront.updates { self.send(["type": "storefront", "storefront": ["country": s.countryCode, "id": s.id]]) }
        }
    }

    // MARK: Calls

    /// Runs a method; reply answers once, on the main thread.
    func call(_ method: String, _ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) {
        start()
        let json = (try? JSONSerialization.jsonObject(with: args.isEmpty ? Data("{}".utf8) : args)) as? [String: Any] ?? [:]
        switch method {
        case "next":
            if !events.isEmpty { reply(.success(events.removeFirst())); return }
            waiting?(.failure(Failure("stopped")))
            waiting = reply
            return
        case "stop":
            waiting?(.failure(Failure("stopped")))
            waiting = nil
            reply(.success(Data()))
            return
        case "available":
            reply(.success(Data([AppStore.canMakePayments ? 1 : 0])))
            return
        default:
            break
        }
        Task { @MainActor in
            do {
                let out: Any? = try await self.run(method, json)
                if let out {
                    reply(.success(try JSONSerialization.data(withJSONObject: out)))
                } else {
                    reply(.success(Data()))
                }
            } catch {
                reply(.failure(Self.failure(error)))
            }
        }
    }

    private func run(_ method: String, _ a: [String: Any]) async throws -> Any? {
        switch method {
        case "products":
            let items = (a["items"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? String }
            let got = try await Product.products(for: items)
            for p in got { products[p.id] = p }
            // In the order asked for: StoreKit's is its own.
            var out: [[String: Any]] = []
            var missing: [String] = []
            for id in items {
                if let p = got.first(where: { $0.id == id }) { out.append(await wire(p)) } else { missing.append(id) }
            }
            return ["products": out, "missing": missing]
        case "buy":
            return try await buy(a)
        case "finish":
            guard let s = a["id"] as? String, let id = UInt64(s) else { throw Failure("no transaction") }
            if let t = unfinished.removeValue(forKey: id) {
                await t.finish()
                return nil
            }
            for await v in Transaction.unfinished {
                let t = v.unsafePayloadValue
                if t.id == id { await t.finish() }
            }
            return nil
        case "entitlements":
            return await entitlements()
        case "restore":
            try await AppStore.sync()
            return await entitlements()
        case "status":
            return try await status(a["group"] as? String ?? "")
        case "manage":
            try await manage(a["productId"] as? String ?? "")
            return nil
        case "refund":
            guard let s = a["id"] as? String, let id = UInt64(s) else { throw Failure("no transaction") }
            return ["requested": try await refund(id)]
        case "redeem":
            try await redeem()
            return nil
        case "storefront":
            guard let s = await Storefront.current else { throw Failure("not available: no storefront (not signed in to the App Store)") }
            return ["country": s.countryCode, "id": s.id]
        default:
            throw Failure("no module method purchase.\(method)")
        }
    }

    /// An error as the Go package reads it (its prefixes: "not
    /// available", "not allowed", "unknown product").
    static func failure(_ e: Error) -> Error {
        if let e = e as? Failure { return e }
        if let e = e as? Product.PurchaseError {
            switch e {
            case .productUnavailable: return Failure("unknown product")
            case .purchaseNotAllowed: return Failure("not allowed: \(e.localizedDescription)")
            default: return Failure(e.localizedDescription)
            }
        }
        if let e = e as? StoreKitError, case .notAvailableInStorefront = e {
            return Failure("unknown product: not in this storefront")
        }
        return Failure(e.localizedDescription)
    }

    private func product(_ id: String) async throws -> Product {
        if let p = products[id] { return p }
        guard let p = try await Product.products(for: [id]).first else { throw Failure("unknown product: \(id)") }
        products[id] = p
        return p
    }

    private func buy(_ a: [String: Any]) async throws -> Any {
        guard let id = a["id"] as? String else { throw Failure("no product") }
        let p = try await product(id)
        guard AppStore.canMakePayments else { throw Failure("not allowed: payments are turned off") }
        var options = Set<Product.PurchaseOption>()
        if let q = a["quantity"] as? Int, q > 1 { options.insert(.quantity(q)) }
        if let s = a["accountToken"] as? String, !s.isEmpty {
            guard let u = UUID(uuidString: s) else { throw Failure("Options.AccountToken must be a UUID on Apple's platforms") }
            options.insert(.appAccountToken(u))
        }
        let offerID = a["offerId"] as? String ?? ""
        if let sig = a["signature"] as? [String: Any], !offerID.isEmpty {
            if let jws = sig["jws"] as? String, !jws.isEmpty {
                options.formUnion(Product.PurchaseOption.promotionalOffer(offerID, compactJWS: jws))
            } else {
                guard let key = sig["keyId"] as? String, let n = sig["nonce"] as? String, let nonce = UUID(uuidString: n),
                      let b64 = sig["signature"] as? String, let data = Data(base64Encoded: b64) else {
                    throw Failure("a promotional offer's signature needs its key ID, nonce (a UUID) and signature")
                }
                let ts = (sig["timestamp"] as? NSNumber)?.intValue ?? 0
                options.insert(.promotionalOffer(offerID: offerID, keyID: key, nonce: nonce, signature: data, timestamp: ts))
            }
        } else if a["offerType"] as? String == "winBack", #available(iOS 18.0, macOS 15.0, *) {
            if let o = p.subscription?.winBackOffers.first(where: { $0.id == offerID }) { options.insert(.winBackOffer(o)) }
        }
        let result: Product.PurchaseResult
        #if canImport(UIKit)
        if #available(iOS 17.0, *), let scene = scene() {
            result = try await p.purchase(confirmIn: scene, options: options)
        } else {
            result = try await p.purchase(options: options)
        }
        #else
        if #available(macOS 15.2, *), let w = window() {
            result = try await p.purchase(confirmIn: w, options: options)
        } else {
            result = try await p.purchase(options: options)
        }
        #endif
        switch result {
        case .success(let v):
            let t = v.unsafePayloadValue
            given.insert(key(t))
            unfinished[t.id] = t
            return ["status": "purchased", "transaction": wire(v)]
        case .pending:
            return ["status": "pending"]
        case .userCancelled:
            return ["status": "cancelled"]
        @unknown default:
            return ["status": "cancelled"]
        }
    }

    private func entitlements() async -> [[String: Any]] {
        var out: [[String: Any]] = []
        for await v in Transaction.currentEntitlements { out.append(wire(v)) }
        return out
    }

    private func status(_ group: String) async throws -> [[String: Any]] {
        var statuses: [Product.SubscriptionInfo.Status] = []
        if #available(iOS 17.0, macOS 14.0, *) {
            statuses = try await Product.SubscriptionInfo.status(for: group)
        } else if let p = products.values.first(where: { $0.subscription?.subscriptionGroupID == group }) {
            statuses = try await p.subscription?.status ?? []
        }
        return statuses.map { s in
            var d: [String: Any] = ["state": Self.state(s.state), "transaction": wire(s.transaction)]
            d["productId"] = s.transaction.unsafePayloadValue.productID
            let r = s.renewalInfo.unsafePayloadValue
            d["willRenew"] = r.willAutoRenew
            if let to = r.autoRenewPreference { d["renewsTo"] = to }
            if let g = r.gracePeriodExpirationDate { d["graceExpiresMs"] = Self.ms(g) }
            if #available(iOS 17.0, macOS 14.0, *), let at = r.renewalDate { d["renewalMs"] = Self.ms(at) }
            if let why = r.expirationReason { d["expirationReason"] = Self.reason(why) }
            d["renewalJws"] = s.renewalInfo.jwsRepresentation
            return d
        }
    }

    private func manage(_ productID: String) async throws {
        #if canImport(UIKit)
        guard let scene = scene() else { throw Failure("no screen") }
        if #available(iOS 17.0, *), let g = products[productID]?.subscription?.subscriptionGroupID {
            try await AppStore.showManageSubscriptions(in: scene, subscriptionGroupID: g)
        } else {
            try await AppStore.showManageSubscriptions(in: scene)
        }
        #else
        // The Mac has no sheet: the App Store's subscriptions page.
        NSWorkspace.shared.open(URL(string: "macappstore://apps.apple.com/account/subscriptions")!)
        #endif
    }

    private func refund(_ id: UInt64) async throws -> Bool {
        #if canImport(UIKit)
        guard let scene = scene() else { throw Failure("no screen") }
        let s = try await Transaction.beginRefundRequest(for: id, in: scene)
        #else
        guard let c = window()?.contentViewController else { throw Failure("no window") }
        let s = try await Transaction.beginRefundRequest(for: id, in: c)
        #endif
        return s == .success
    }

    private func redeem() async throws {
        #if canImport(UIKit)
        guard let scene = scene() else { throw Failure("no screen") }
        try await AppStore.presentOfferCodeRedeemSheet(in: scene)
        #else
        guard #available(macOS 15.0, *) else { throw Failure("not available: offer codes need macOS 15") }
        guard let c = window()?.contentViewController else { throw Failure("no window") }
        try await AppStore.presentOfferCodeRedeemSheet(from: c)
        #endif
    }

    // MARK: Updates

    private func key(_ t: Transaction) -> String { "\(t.id):\(t.revocationDate == nil ? "" : "revoked")" }

    /// A transaction outside a purchase: kept, then given to "next".
    private func updated(_ v: VerificationResult<Transaction>) {
        let t = v.unsafePayloadValue
        guard given.insert(key(t)).inserted else { return }
        if t.revocationDate == nil { unfinished[t.id] = t }
        send(["type": "transaction", "transaction": wire(v)])
    }

    private func send(_ event: [String: Any]) {
        guard let b = try? JSONSerialization.data(withJSONObject: event) else { return }
        if let w = waiting {
            waiting = nil
            w(.success(b))
        } else {
            events.append(b)
        }
    }

    // MARK: The wire

    static func ms(_ d: Date) -> Int64 { Int64((d.timeIntervalSince1970 * 1000).rounded()) }

    static func micros(_ d: Decimal) -> Int64 {
        NSDecimalNumber(decimal: d * 1_000_000).int64Value
    }

    static func kind(_ t: Product.ProductType) -> String {
        switch t {
        case .consumable: return "consumable"
        case .autoRenewable: return "autoRenewable"
        case .nonRenewable: return "nonRenewing"
        default: return "nonConsumable"
        }
    }

    static func period(_ p: Product.SubscriptionPeriod) -> [String: Any] {
        let unit: String
        switch p.unit {
        case .day: unit = "day"
        case .week: unit = "week"
        case .month: unit = "month"
        default: unit = "year"
        }
        return ["unit": unit, "count": p.value]
    }

    static func mode(_ m: Product.SubscriptionOffer.PaymentMode) -> String {
        switch m {
        case .freeTrial: return "freeTrial"
        case .payAsYouGo: return "payAsYouGo"
        case .payUpFront: return "payUpFront"
        default: return "recurring"
        }
    }

    static func state(_ s: Product.SubscriptionInfo.RenewalState) -> String {
        switch s {
        case .subscribed: return "subscribed"
        case .inBillingRetryPeriod: return "billingRetry"
        case .inGracePeriod: return "gracePeriod"
        case .revoked: return "revoked"
        default: return "expired"
        }
    }

    static func reason(_ r: Product.SubscriptionInfo.RenewalInfo.ExpirationReason) -> String {
        switch r {
        case .autoRenewDisabled: return "autoRenewDisabled"
        case .billingError: return "billingError"
        case .didNotConsentToPriceIncrease: return "didNotConsentToPriceIncrease"
        case .productUnavailable: return "productUnavailable"
        default: return "unknown"
        }
    }

    private func offer(_ o: Product.SubscriptionOffer, _ type: String, _ currency: String) -> [String: Any] {
        var d: [String: Any] = ["type": type, "phases": [[
            "price": o.displayPrice, "micros": Self.micros(o.price), "currency": currency,
            "period": Self.period(o.period), "cycles": o.periodCount, "mode": Self.mode(o.paymentMode),
        ] as [String: Any]]]
        if let id = o.id { d["id"] = id }
        return d
    }

    private func wire(_ p: Product) async -> [String: Any] {
        let currency = p.priceFormatStyle.currencyCode
        var d: [String: Any] = ["id": p.id, "kind": Self.kind(p.type), "title": p.displayName, "description": p.description,
                                "price": p.displayPrice, "micros": Self.micros(p.price), "currency": currency,
                                "familyShareable": p.isFamilyShareable]
        if let s = p.subscription {
            d["group"] = s.subscriptionGroupID
            if #available(iOS 16.4, macOS 13.3, *) { d["level"] = s.groupLevel }
            d["period"] = Self.period(s.subscriptionPeriod)
            var offers: [[String: Any]] = []
            if let i = s.introductoryOffer { offers.append(offer(i, "introductory", currency)) }
            for o in s.promotionalOffers { offers.append(offer(o, "promotional", currency)) }
            if #available(iOS 18.0, macOS 15.0, *) {
                for o in s.winBackOffers { offers.append(offer(o, "winBack", currency)) }
            }
            d["offers"] = offers
            d["introEligible"] = await s.isEligibleForIntroOffer
        }
        return d
    }

    private func wire(_ v: VerificationResult<Transaction>) -> [String: Any] {
        let t = v.unsafePayloadValue
        var verified = false
        if case .verified = v { verified = true }
        var d: [String: Any] = [
            "id": String(t.id), "originalId": String(t.originalID), "productId": t.productID,
            "kind": Self.kind(t.productType), "quantity": t.purchasedQuantity,
            "state": t.revocationDate == nil ? "purchased" : "revoked", "dateMs": Self.ms(t.purchaseDate),
            "upgraded": t.isUpgraded, "familyShared": t.ownershipType == .familyShared,
            "jws": v.jwsRepresentation, "verified": verified,
        ]
        if let e = t.expirationDate { d["expiresMs"] = Self.ms(e) }
        if let r = t.revocationDate { d["revokedMs"] = Self.ms(r) }
        if let r = t.revocationReason { d["revocationReason"] = r == .developerIssue ? "refundedForIssue" : "refundedForOther" }
        if let a = t.appAccountToken { d["accountToken"] = a.uuidString.lowercased() }
        if #available(iOS 16.0, macOS 13.0, *) { d["environment"] = t.environment.rawValue }
        if #available(iOS 17.0, macOS 14.0, *) {
            d["storefront"] = t.storefront.countryCode
        } else {
            d["storefront"] = t.storefrontCountryCode
        }
        if #available(iOS 17.2, macOS 14.2, *) {
            if let o = t.offer {
                if let id = o.id { d["offerId"] = id }
                d["offerType"] = Self.offerType(o.type)
            }
        } else if let ty = t.offerType {
            if let id = t.offerID { d["offerId"] = id }
            d["offerType"] = Self.offerType(ty)
        }
        return d
    }

    static func offerType(_ t: Transaction.OfferType) -> String {
        switch t {
        case .introductory: return "introductory"
        case .promotional: return "promotional"
        case .code: return "code"
        default:
            if #available(iOS 18.0, macOS 15.0, *), t == .winBack { return "winBack" }
            return "promotional"
        }
    }
}

#if targetEnvironment(simulator)
/// A StoreKit configuration file (asuka.json's ios.storekit) in asuka
/// dev's simulator build: the App Store, local, as Xcode's scheme sets it
/// up. The CLI puts the file in the bundle (AsukaStoreKit.storekit) and
/// launches the app as a test run is, Xcode's XCTest found
/// (DYLD_FRAMEWORK_PATH), which StoreKitTest's session (SKTestSession, from
/// the simulator's developer frameworks) needs, and storekitd takes it for
/// an app installed for development (get-task-allow). Launched otherwise
/// (from the home screen), there's no XCTest: storekitd goes on with the
/// configuration it was last given for the app.
@available(macOS 12.0, *)
enum AsukaStoreKitTest {
    static var session: NSObject?

    @MainActor static func start() {
        guard session == nil, let file = Bundle.main.url(forResource: "AsukaStoreKit", withExtension: "storekit") else { return }
        // StoreKitTest loads XCTest from there, and ends the app if it can't.
        guard dlopen("/System/Library/Frameworks/XCTest.framework/XCTest", RTLD_NOW) != nil else {
            NSLog("asuka: the StoreKit configuration file is used when asuka dev launches the app (XCTest is Xcode's)")
            return
        }
        guard dlopen("/Developer/Library/Frameworks/StoreKitTest.framework/StoreKitTest", RTLD_NOW) != nil,
              let cls = NSClassFromString("SKTestSession") as? NSObject.Type else {
            NSLog("asuka: no StoreKitTest in this simulator: the StoreKit configuration file isn't used")
            return
        }
        // [[SKTestSession alloc] initWithContentsOfURL:error:], by their
        // implementations: alloc's object goes to init, whose answer is ours.
        typealias Alloc = @convention(c) (AnyClass, Selector) -> UnsafeMutableRawPointer?
        typealias Init = @convention(c) (UnsafeMutableRawPointer, Selector, NSURL, AutoreleasingUnsafeMutablePointer<NSError?>) -> Unmanaged<NSObject>?
        let allocSel = NSSelectorFromString("alloc"), initSel = NSSelectorFromString("initWithContentsOfURL:error:")
        guard let am = class_getClassMethod(cls, allocSel), let im = class_getInstanceMethod(cls, initSel),
              let obj = unsafeBitCast(method_getImplementation(am), to: Alloc.self)(cls, allocSel) else { return }
        var err: NSError?
        guard let s = unsafeBitCast(method_getImplementation(im), to: Init.self)(obj, initSel, file as NSURL, &err)?.takeRetainedValue() else {
            NSLog("asuka: the StoreKit configuration file: %@", err?.localizedDescription ?? "?")
            return
        }
        s.setValue(false, forKey: "disableDialogs")
        session = s
        NSLog("asuka: selling from the StoreKit configuration file")
    }
}
#endif
#endif
