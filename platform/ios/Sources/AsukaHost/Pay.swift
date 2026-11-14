#if canImport(UIKit) && ASUKA_PAY // an app that calls asuka/sdk/pay (see the CLI's iosFeatures)
import UIKit
import PassKit
import Contacts

/// asuka/sdk/pay's module: Apple Pay, with PKPaymentAuthorizationController
/// (internal/payspec has the wire). "available" answers canMakePayments
/// and whether a card of the request's networks is in Wallet; "setUp"
/// opens Wallet's screen to add one; "present" shows the sheet, whose
/// delegate's callbacks go to Go as events ("next"), each completed when
/// Go answers it ("answer"); "done" when it closes.
@MainActor final class AsukaPay: NSObject, @preconcurrency PKPaymentAuthorizationControllerDelegate {
    static let shared = AsukaPay()

    private var controller: PKPaymentAuthorizationController?
    private var id = ""
    private var seq = 0
    private var request = AsukaPayRequest()
    /// The sheet's summary items and methods now, for the callbacks the
    /// app has no handler for.
    private var items: [PKPaymentSummaryItem] = []
    private var methods: [PKShippingMethod] = []
    private var queue: [Data] = []
    private var waiting: ((Result<Data, Error>) -> Void)?
    private var pending: [Int: (AsukaPayAnswer) -> Void] = [:]

    func methods(_ rt: AsukaRuntime) -> [String: AsukaModuleMethod] {
        [
            "available": { args, reply in
                let p = try? JSONDecoder().decode(AsukaPayPresent.self, from: args)
                let r = p?.apple ?? AsukaPayRequest()
                let can = PKPaymentAuthorizationController.canMakePayments()
                let card = can && PKPaymentAuthorizationController.canMakePayments(usingNetworks: r.paymentNetworks, capabilities: r.merchantCapabilities)
                reply(.success(Data(#"{"wallet":"apple","canPay":\#(can),"hasCard":\#(card)}"#.utf8)))
            },
            "setUp": { _, reply in
                PKPassLibrary().openPaymentSetup()
                reply(.success(Data()))
            },
            "present": { [unowned self] args, reply in self.present(args, reply) },
            "next": { [unowned self] args, reply in
                if String(decoding: args, as: UTF8.self) != self.id || (self.controller == nil && self.queue.isEmpty) {
                    reply(.failure(AsukaModuleError("stopped"))); return
                }
                if !self.queue.isEmpty { reply(.success(self.queue.removeFirst())); return }
                self.waiting?(.failure(AsukaModuleError("stopped")))
                self.waiting = reply
            },
            "answer": { [unowned self] args, reply in
                if let a = try? JSONDecoder().decode(AsukaPayAnswer.self, from: args), a.id == self.id,
                   let done = self.pending.removeValue(forKey: a.seq) {
                    done(a)
                }
                reply(.success(Data()))
            },
        ]
    }

    private func present(_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) {
        if controller != nil { reply(.failure(AsukaModuleError("busy"))); return }
        let p: AsukaPayPresent
        do { p = try JSONDecoder().decode(AsukaPayPresent.self, from: args) } catch { reply(.failure(error)); return }
        if let e = p.appleError, !e.isEmpty { reply(.failure(AsukaModuleError(e))); return }
        guard PKPaymentAuthorizationController.canMakePayments() else {
            reply(.failure(AsukaModuleError("not available: Apple Pay isn't on this device, or it's restricted"))); return
        }
        request = p.apple
        id = p.id
        seq = 0
        queue = []
        pending = [:]
        let r = request.paymentRequest()
        items = r.paymentSummaryItems
        methods = r.shippingMethods ?? []
        let c = PKPaymentAuthorizationController(paymentRequest: r)
        c.delegate = self
        controller = c
        c.present { ok in
            DispatchQueue.main.async {
                if ok { reply(.success(Data())); return }
                AsukaPay.shared.controller = nil
                reply(.failure(AsukaModuleError("not available: the payment sheet didn't show (check the merchant ID and the networks)")))
            }
        }
    }

    /// send gives Go an event; done runs with its answer.
    private func send(_ event: [String: Any], _ done: ((AsukaPayAnswer) -> Void)? = nil) {
        var e = event
        if let done {
            seq += 1
            e["seq"] = seq
            pending[seq] = done
        }
        let b = (try? JSONSerialization.data(withJSONObject: e)) ?? Data("{}".utf8)
        if let w = waiting { waiting = nil; w(.success(b)) } else { queue.append(b) }
    }

    /// keep takes an answer's items and methods as the sheet's now.
    private func keep(_ a: AsukaPayAnswer) {
        if let i = a.items { items = AsukaPayRequest.summaryItems(i) }
        if let m = a.shippingMethods { methods = m.map { $0.method() } }
    }

    // The delegate.

    func paymentAuthorizationController(_ c: PKPaymentAuthorizationController, didAuthorizePayment payment: PKPayment,
                                        handler completion: @escaping (PKPaymentAuthorizationResult) -> Void) {
        send(["type": "authorize", "payment": AsukaPay.payment(payment)]) { a in
            let errors = a.errors ?? []
            completion(PKPaymentAuthorizationResult(status: errors.isEmpty ? .success : .failure, errors: errors.map { $0.error() }))
        }
    }

    func paymentAuthorizationController(_ c: PKPaymentAuthorizationController, didSelectShippingContact contact: PKContact,
                                        handler completion: @escaping (PKPaymentRequestShippingContactUpdate) -> Void) {
        guard request.events.shippingContact == true else {
            completion(PKPaymentRequestShippingContactUpdate(errors: nil, paymentSummaryItems: items, shippingMethods: methods)); return
        }
        send(["type": "shippingContact", "contact": AsukaPay.contact(contact)]) { [unowned self] a in
            self.keep(a)
            completion(PKPaymentRequestShippingContactUpdate(errors: (a.errors ?? []).map { $0.error() }, paymentSummaryItems: self.items, shippingMethods: self.methods))
        }
    }

    func paymentAuthorizationController(_ c: PKPaymentAuthorizationController, didSelectShippingMethod method: PKShippingMethod,
                                        handler completion: @escaping (PKPaymentRequestShippingMethodUpdate) -> Void) {
        guard request.events.shippingMethod == true else {
            completion(PKPaymentRequestShippingMethodUpdate(paymentSummaryItems: items)); return
        }
        send(["type": "shippingMethod", "method": method.identifier ?? ""]) { [unowned self] a in
            self.keep(a)
            let u = PKPaymentRequestShippingMethodUpdate(paymentSummaryItems: self.items)
            if !(a.errors ?? []).isEmpty { u.status = .failure }
            completion(u)
        }
    }

    func paymentAuthorizationController(_ c: PKPaymentAuthorizationController, didSelectPaymentMethod method: PKPaymentMethod,
                                        handler completion: @escaping (PKPaymentRequestPaymentMethodUpdate) -> Void) {
        guard request.events.paymentMethod == true else {
            completion(PKPaymentRequestPaymentMethodUpdate(errors: nil, paymentSummaryItems: items)); return
        }
        send(["type": "paymentMethod", "card": AsukaPay.card(method)]) { [unowned self] a in
            self.keep(a)
            completion(PKPaymentRequestPaymentMethodUpdate(errors: (a.errors ?? []).map { $0.error() }, paymentSummaryItems: self.items))
        }
    }

    @available(iOS 15.0, *)
    func paymentAuthorizationController(_ c: PKPaymentAuthorizationController, didChangeCouponCode code: String,
                                        handler completion: @escaping (PKPaymentRequestCouponCodeUpdate) -> Void) {
        guard request.events.coupon == true else {
            completion(PKPaymentRequestCouponCodeUpdate(errors: nil, paymentSummaryItems: items, shippingMethods: methods)); return
        }
        send(["type": "coupon", "coupon": code]) { [unowned self] a in
            self.keep(a)
            completion(PKPaymentRequestCouponCodeUpdate(errors: (a.errors ?? []).map { $0.error() }, paymentSummaryItems: self.items, shippingMethods: self.methods))
        }
    }

    func paymentAuthorizationControllerDidFinish(_ c: PKPaymentAuthorizationController) {
        c.dismiss(completion: nil)
        guard c === controller else { return }
        controller = nil
        pending = [:]
        send(["type": "done"])
    }

    func presentationWindow(for c: PKPaymentAuthorizationController) -> UIWindow? {
        AsukaRuntime.shared.controller?.view.window
    }

    // PassKit's objects as JSON.

    static func payment(_ p: PKPayment) -> [String: Any] {
        var d: [String: Any] = [
            "token": String(decoding: p.token.paymentData, as: UTF8.self),
            "transactionId": p.token.transactionIdentifier,
            "card": card(p.token.paymentMethod),
        ]
        if let s = p.shippingContact { d["shipping"] = contact(s) }
        if let b = p.billingContact { d["billing"] = contact(b) }
        if let m = p.shippingMethod?.identifier { d["shippingMethod"] = m }
        return d
    }

    static func card(_ m: PKPaymentMethod) -> [String: Any] {
        var d: [String: Any] = ["name": m.displayName ?? ""]
        if let n = m.network { d["network"] = AsukaPayRequest.networkName(n) }
        switch m.type {
        case .debit: d["type"] = "debit"
        case .credit: d["type"] = "credit"
        case .prepaid: d["type"] = "prepaid"
        case .store: d["type"] = "store"
        case .eMoney: d["type"] = "eMoney"
        default: break
        }
        if let b = m.billingAddress {
            var c: [String: Any] = ["givenName": b.givenName, "familyName": b.familyName]
            if let a = b.postalAddresses.first?.value { c["address"] = address(a) }
            d["billing"] = c
        }
        return d
    }

    static func contact(_ c: PKContact) -> [String: Any] {
        var d: [String: Any] = [:]
        if let n = c.name {
            d["givenName"] = n.givenName ?? ""
            d["familyName"] = n.familyName ?? ""
            d["name"] = PersonNameComponentsFormatter().string(from: n)
        }
        if let e = c.emailAddress { d["email"] = e }
        if let p = c.phoneNumber?.stringValue { d["phone"] = p }
        if let a = c.postalAddress { d["address"] = address(a) }
        return d
    }

    static func address(_ a: CNPostalAddress) -> [String: Any] {
        ["street": a.street, "subLocality": a.subLocality, "city": a.city, "subAdministrativeArea": a.subAdministrativeArea,
         "state": a.state, "postalCode": a.postalCode, "country": a.country, "countryCode": a.isoCountryCode]
    }
}

// The wire (internal/payspec).

struct AsukaPayPresent: Decodable {
    var id = ""
    var apple = AsukaPayRequest()
    var appleError: String?
}

struct AsukaPayEventsOn: Decodable {
    var shippingContact: Bool?, shippingMethod: Bool?, paymentMethod: Bool?, coupon: Bool?
}

struct AsukaPayItem: Decodable {
    var label = "", amount = "0"
    var pending: Bool?
    var kind: String?
    var startMs: Int64?, endMs: Int64?, dateMs: Int64?
    var interval: String?
    var every: Int?
    var threshold: String?

    static func decimal(_ s: String?) -> NSDecimalNumber {
        let d = NSDecimalNumber(string: s ?? "0", locale: Locale(identifier: "en_US_POSIX"))
        return d == .notANumber ? .zero : d
    }
    static func date(_ ms: Int64?) -> Date? { ms.flatMap { $0 > 0 ? Date(timeIntervalSince1970: Double($0) / 1000) : nil } }

    func item() -> PKPaymentSummaryItem {
        let amount = AsukaPayItem.decimal(self.amount)
        let type: PKPaymentSummaryItemType = pending == true ? .pending : .final
        switch kind {
        case "recurring":
            if #available(iOS 15.0, *) {
                let r = PKRecurringPaymentSummaryItem(label: label, amount: amount, type: type)
                r.startDate = AsukaPayItem.date(startMs)
                r.endDate = AsukaPayItem.date(endMs)
                switch interval {
                case "day": r.intervalUnit = .day
                case "week": r.intervalUnit = .weekOfYear
                case "year": r.intervalUnit = .year
                default: r.intervalUnit = .month
                }
                r.intervalCount = max(every ?? 1, 1)
                return r
            }
        case "deferred":
            if #available(iOS 17.0, *), let d = AsukaPayItem.date(dateMs) {
                let r = PKDeferredPaymentSummaryItem(label: label, amount: amount, type: type)
                r.deferredDate = d
                return r
            }
        case "reload":
            if #available(iOS 16.0, *) {
                let r = PKAutomaticReloadPaymentSummaryItem(label: label, amount: amount, type: type)
                r.thresholdAmount = AsukaPayItem.decimal(threshold)
                return r
            }
        default: break
        }
        return PKPaymentSummaryItem(label: label, amount: amount, type: type)
    }
}

struct AsukaPayShippingMethod: Decodable {
    var id = "", label = "", amount = "0"
    var detail: String?, from: String?, to: String?

    func method() -> PKShippingMethod {
        let m = PKShippingMethod(label: label, amount: AsukaPayItem.decimal(amount))
        m.identifier = id
        m.detail = detail
        if #available(iOS 15.0, *), let f = AsukaPayShippingMethod.day(from), let t = AsukaPayShippingMethod.day(to ?? from) {
            m.dateComponentsRange = PKDateComponentsRange(start: f, end: t)
        }
        return m
    }

    /// day is "2026-11-20" as date components.
    static func day(_ s: String?) -> DateComponents? {
        guard let p = s?.split(separator: "-").compactMap({ Int($0) }), p.count == 3 else { return nil }
        return DateComponents(calendar: Calendar(identifier: .gregorian), year: p[0], month: p[1], day: p[2])
    }
}

/// A recurring, deferred or automatic reload request (regular is the
/// first's billing, billing the others').
struct AsukaPayRecurring: Decodable {
    var description = "", managementURL = ""
    var regular: AsukaPayItem?
    var trial: AsukaPayItem?
    var billing: AsukaPayItem?
    var billingAgreement: String?, tokenNotificationURL: String?
    var freeCancellationMs: Int64?
}

struct AsukaPayRequest: Decodable {
    var merchant = "", country = "US", currency = "USD"
    var networks: [String] = []
    var capabilities: [String] = []
    var cardCountries: [String]?
    var items: [AsukaPayItem] = []
    var shippingFields: [String]?, billingFields: [String]?
    var shippingType: String?
    var shippingMethods: [AsukaPayShippingMethod]?
    var shippingFixed: Bool?
    var coupons: Bool?
    var coupon: String?
    var applicationData: String?
    var recurring: AsukaPayRecurring?, deferred: AsukaPayRecurring?, reload: AsukaPayRecurring?
    var events = AsukaPayEventsOn()

    /// The networks by asuka/sdk/pay's names.
    static let networkNames: [(String, PKPaymentNetwork)] = {
        var n: [(String, PKPaymentNetwork)] = [
            ("visa", .visa), ("mastercard", .masterCard), ("amex", .amex), ("discover", .discover), ("jcb", .JCB),
            ("interac", .interac), ("unionpay", .chinaUnionPay), ("maestro", .maestro), ("electron", .electron),
            ("elo", .elo), ("cartesBancaires", .cartesBancaires), ("eftpos", .eftpos), ("mada", .mada), ("mir", .mir),
            ("girocard", .girocard), ("privateLabel", .privateLabel), ("dankort", .dankort),
        ]
        if #available(iOS 16.0, *) { n.append(("bancontact", .bancontact)) }
        return n
    }()

    static func networkName(_ n: PKPaymentNetwork) -> String {
        networkNames.first { $0.1 == n }?.0 ?? n.rawValue.lowercased()
    }

    var paymentNetworks: [PKPaymentNetwork] {
        networks.compactMap { name in AsukaPayRequest.networkNames.first { $0.0 == name }?.1 }
    }

    var merchantCapabilities: PKMerchantCapability {
        var c: PKMerchantCapability = []
        for s in capabilities {
            switch s {
            case "3DS": c.insert(.threeDSecure)
            case "EMV": c.insert(.emv)
            case "credit": c.insert(.credit)
            case "debit": c.insert(.debit)
            default: break
            }
        }
        return c.isEmpty ? .threeDSecure : c
    }

    static func fields(_ names: [String]?) -> Set<PKContactField> {
        var s = Set<PKContactField>()
        for n in names ?? [] {
            switch n {
            case "name": s.insert(.name)
            case "email": s.insert(.emailAddress)
            case "phone": s.insert(.phoneNumber)
            case "address": s.insert(.postalAddress)
            case "phoneticName": s.insert(.phoneticName)
            default: break
            }
        }
        return s
    }

    static func summaryItems(_ items: [AsukaPayItem]) -> [PKPaymentSummaryItem] { items.map { $0.item() } }

    func paymentRequest() -> PKPaymentRequest {
        let r = PKPaymentRequest()
        r.merchantIdentifier = merchant
        r.countryCode = country
        r.currencyCode = currency
        r.supportedNetworks = paymentNetworks
        r.merchantCapabilities = merchantCapabilities
        if let c = cardCountries, !c.isEmpty { r.supportedCountries = Set(c) }
        r.paymentSummaryItems = AsukaPayRequest.summaryItems(items)
        r.requiredShippingContactFields = AsukaPayRequest.fields(shippingFields)
        r.requiredBillingContactFields = AsukaPayRequest.fields(billingFields)
        switch shippingType {
        case "delivery": r.shippingType = .delivery
        case "storePickup": r.shippingType = .storePickup
        case "servicePickup": r.shippingType = .servicePickup
        default: r.shippingType = .shipping
        }
        if let m = shippingMethods, !m.isEmpty { r.shippingMethods = m.map { $0.method() } }
        if #available(iOS 15.0, *) {
            r.shippingContactEditingMode = shippingFixed == true ? .storePickup : .available
            r.supportsCouponCode = coupons == true
            if let c = coupon, !c.isEmpty { r.couponCode = c }
        }
        if let a = applicationData { r.applicationData = Data(base64Encoded: a) }
        if #available(iOS 16.0, *) {
            if let x = recurring, let url = URL(string: x.managementURL),
               let regular = x.regular?.item() as? PKRecurringPaymentSummaryItem {
                let rr = PKRecurringPaymentRequest(paymentDescription: x.description, regularBilling: regular, managementURL: url)
                rr.trialBilling = x.trial?.item() as? PKRecurringPaymentSummaryItem
                rr.billingAgreement = x.billingAgreement
                rr.tokenNotificationURL = x.tokenNotificationURL.flatMap(URL.init(string:))
                r.recurringPaymentRequest = rr
            }
            if let x = reload, let url = URL(string: x.managementURL),
               let billing = x.billing?.item() as? PKAutomaticReloadPaymentSummaryItem {
                let rr = PKAutomaticReloadPaymentRequest(paymentDescription: x.description, automaticReloadBilling: billing, managementURL: url)
                rr.billingAgreement = x.billingAgreement
                rr.tokenNotificationURL = x.tokenNotificationURL.flatMap(URL.init(string:))
                r.automaticReloadPaymentRequest = rr
            }
        }
        if #available(iOS 17.0, *), let x = deferred, let url = URL(string: x.managementURL),
           let billing = x.billing?.item() as? PKDeferredPaymentSummaryItem {
            let d = PKDeferredPaymentRequest(paymentDescription: x.description, deferredBilling: billing, managementURL: url)
            d.billingAgreement = x.billingAgreement
            d.tokenNotificationURL = x.tokenNotificationURL.flatMap(URL.init(string:))
            if let f = AsukaPayItem.date(x.freeCancellationMs) {
                d.freeCancellationDate = f
                d.freeCancellationDateTimeZone = .current
            }
            r.deferredPaymentRequest = d
        }
        return r
    }
}

struct AsukaPayFieldError: Decodable {
    var field = "", message = ""

    /// error is PassKit's error for the field, which the sheet shows by it.
    func error() -> Error {
        let shipping: [String: String] = ["shippingStreet": CNPostalAddressStreetKey, "shippingCity": CNPostalAddressCityKey,
                                          "shippingState": CNPostalAddressStateKey, "shippingPostalCode": CNPostalAddressPostalCodeKey,
                                          "shippingCountry": CNPostalAddressISOCountryCodeKey]
        let billing: [String: String] = ["billingAddress": CNPostalAddressStreetKey, "billingStreet": CNPostalAddressStreetKey,
                                         "billingCity": CNPostalAddressCityKey, "billingState": CNPostalAddressStateKey,
                                         "billingPostalCode": CNPostalAddressPostalCodeKey, "billingCountry": CNPostalAddressISOCountryCodeKey]
        if let k = shipping[field] { return PKPaymentRequest.paymentShippingAddressInvalidError(withKey: k, localizedDescription: message) }
        if let k = billing[field] { return PKPaymentRequest.paymentBillingAddressInvalidError(withKey: k, localizedDescription: message) }
        switch field {
        case "shippingAddress": return PKPaymentRequest.paymentContactInvalidError(withContactField: .postalAddress, localizedDescription: message)
        case "shippingName": return PKPaymentRequest.paymentContactInvalidError(withContactField: .name, localizedDescription: message)
        case "shippingEmail": return PKPaymentRequest.paymentContactInvalidError(withContactField: .emailAddress, localizedDescription: message)
        case "shippingPhone": return PKPaymentRequest.paymentContactInvalidError(withContactField: .phoneNumber, localizedDescription: message)
        case "unserviceable": return PKPaymentRequest.paymentShippingAddressUnserviceableError(withLocalizedDescription: message)
        case "billingName":
            return NSError(domain: PKPaymentErrorDomain, code: PKPaymentError.billingContactInvalidError.rawValue,
                           userInfo: [PKPaymentErrorKey.contactFieldUserInfoKey.rawValue: PKContactField.name, NSLocalizedDescriptionKey: message])
        case "couponInvalid":
            if #available(iOS 15.0, *) { return PKPaymentRequest.paymentCouponCodeInvalidError(localizedDescription: message) }
        case "couponExpired":
            if #available(iOS 15.0, *) { return PKPaymentRequest.paymentCouponCodeExpiredError(localizedDescription: message) }
        default: break
        }
        return NSError(domain: PKPaymentErrorDomain, code: PKPaymentError.unknownError.rawValue, userInfo: [NSLocalizedDescriptionKey: message])
    }
}

struct AsukaPayAnswer: Decodable {
    var id = ""
    var seq = 0
    var items: [AsukaPayItem]?
    var shippingMethods: [AsukaPayShippingMethod]?
    var errors: [AsukaPayFieldError]?
}

/// pay.Button: a PKPaymentButton, made again when its type or style
/// changes (they're fixed at init). Props: appleType (PKPaymentButtonType),
/// style (automatic, black, white, white outline), cornerRadius (-1: the
/// system's); event: press.
@MainActor final class AsukaPayButton: AsukaNativeView {
    let ctx: AsukaNativeContext
    let box = AsukaPayButtonBox()
    var view: UIView { box }
    private var type = 0, style = 0
    private var radius: CGFloat = -1

    init(_ ctx: AsukaNativeContext) {
        self.ctx = ctx
        make()
    }

    func set(_ name: String, _ value: AsukaNativeValue) {
        switch name {
        case "appleType": type = Int(value.int)
        case "style": style = Int(value.int)
        case "cornerRadius":
            radius = value.float >= 0 ? CGFloat(value.float) : -1
            if radius >= 0 { box.button?.cornerRadius = radius }
            return
        default: return
        }
        make()
    }

    private func make() {
        let t = PKPaymentButtonType(rawValue: type) ?? .plain
        // Automatic follows the appearance (iOS 14); the rest are fixed.
        let s: PKPaymentButtonStyle = [.automatic, .black, .white, .whiteOutline][min(max(style, 0), 3)]
        let b = PKPaymentButton(paymentButtonType: t, paymentButtonStyle: s)
        if radius >= 0 { b.cornerRadius = radius }
        b.addTarget(self, action: #selector(pressed), for: .touchUpInside)
        box.button = b
    }

    @objc private func pressed() { ctx.emit("press") }
}

final class AsukaPayButtonBox: UIView {
    var button: PKPaymentButton? {
        didSet {
            oldValue?.removeFromSuperview()
            guard let b = button else { return }
            b.frame = bounds
            b.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            addSubview(b)
        }
    }
    /// As wide as it's given (at least Apple's 140), 44 tall (Apple's
    /// guidelines: 30 at least).
    override func sizeThatFits(_ size: CGSize) -> CGSize {
        CGSize(width: max(min(size.width, 600), 140), height: 44)
    }
}
#endif
