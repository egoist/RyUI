#if canImport(UIKit) && RYUI_APPLEAUTH // an app that calls ryui/appleauth (see the CLI's iosFeatures)
import UIKit
import AuthenticationServices

/// ryui/appleauth's module: "available"; "signIn" (JSON: fullName, email,
/// nonce, state) shows Sign in with Apple and answers the credential as
/// JSON, or fails with "canceled"; "state" (the user ID) answers u8:
/// revoked, authorized, not found, transferred.
@MainActor final class RyUIAppleAuth: NSObject, ASAuthorizationControllerDelegate, ASAuthorizationControllerPresentationContextProviding {
    static let shared = RyUIAppleAuth()
    private var pending: [ObjectIdentifier: (ASAuthorizationController, (Result<Data, Error>) -> Void)] = [:]

    func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            "available": { _, reply in reply(.success(Data([1]))) },
            "signIn": { [unowned self] args, reply in
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let req = ASAuthorizationAppleIDProvider().createRequest()
                var scopes: [ASAuthorization.Scope] = []
                if o["fullName"] as? Bool == true { scopes.append(.fullName) }
                if o["email"] as? Bool == true { scopes.append(.email) }
                req.requestedScopes = scopes
                if let n = o["nonce"] as? String, !n.isEmpty { req.nonce = n }
                if let s = o["state"] as? String, !s.isEmpty { req.state = s }
                let c = ASAuthorizationController(authorizationRequests: [req])
                c.delegate = self
                c.presentationContextProvider = self
                self.pending[ObjectIdentifier(c)] = (c, reply)
                c.performRequests()
            },
            "state": { args, reply in
                ASAuthorizationAppleIDProvider().getCredentialState(forUserID: String(decoding: args, as: UTF8.self)) { s, error in
                    if let error, s != .notFound { reply(.failure(error)); return }
                    let code: UInt8
                    switch s {
                    case .revoked: code = 0
                    case .authorized: code = 1
                    case .transferred: code = 3
                    default: code = 2
                    }
                    reply(.success(Data([code])))
                }
            },
        ]
    }

    func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
        RyUIRuntime.shared.controller?.view.window ?? ASPresentationAnchor()
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithAuthorization authorization: ASAuthorization) {
        guard let (_, reply) = pending.removeValue(forKey: ObjectIdentifier(controller)) else { return }
        guard let c = authorization.credential as? ASAuthorizationAppleIDCredential else {
            reply(.failure(RyUIModuleError("not an Apple ID credential"))); return
        }
        let d: [String: Any] = [
            "user": c.user,
            "identityToken": c.identityToken.map { String(decoding: $0, as: UTF8.self) } ?? "",
            "authorizationCode": c.authorizationCode.map { String(decoding: $0, as: UTF8.self) } ?? "",
            "state": c.state ?? "",
            "email": c.email ?? "",
            "givenName": c.fullName?.givenName ?? "",
            "familyName": c.fullName?.familyName ?? "",
            "realUser": c.realUserStatus == .likelyReal,
        ]
        reply(.success((try? JSONSerialization.data(withJSONObject: d)) ?? Data("{}".utf8)))
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: Error) {
        guard let (_, reply) = pending.removeValue(forKey: ObjectIdentifier(controller)) else { return }
        let code = (error as? ASAuthorizationError)?.code
        reply(.failure(code == .canceled ? RyUIModuleError("canceled") : error))
    }
}

/// appleauth.Button: ASAuthorizationAppleIDButton, made again when its
/// type or style changes (they're fixed at init). Props: type (sign in,
/// continue, sign up), style (black, white, white outline, automatic),
/// cornerRadius; event: press.
@MainActor final class RyUIAppleSignInButton: RyUINativeView {
    let ctx: RyUINativeContext
    let box = RyUIAppleSignInBox()
    var view: UIView { box }
    private var type = 0, style = 0
    private var radius: CGFloat = -1

    init(_ ctx: RyUINativeContext) {
        self.ctx = ctx
        make()
        box.schemeChanged = { [weak self] in if self?.style == 3 { self?.make() } }
    }

    func set(_ name: String, _ value: RyUINativeValue) {
        switch name {
        case "type": type = Int(value.int)
        case "style": style = Int(value.int)
        case "cornerRadius":
            radius = value.float > 0 ? CGFloat(value.float) : -1
            if radius >= 0 { box.button?.cornerRadius = radius }
            return
        default: return
        }
        make()
    }

    private func make() {
        let types: [ASAuthorizationAppleIDButton.ButtonType] = [.signIn, .continue, .signUp]
        // Automatic: white in dark mode, black in light (made again as it changes).
        let dark = box.traitCollection.userInterfaceStyle == .dark
        let styles: [ASAuthorizationAppleIDButton.Style] = [.black, .white, .whiteOutline, dark ? .white : .black]
        let b = ASAuthorizationAppleIDButton(authorizationButtonType: types[min(max(type, 0), 2)], authorizationButtonStyle: styles[min(max(style, 0), 3)])
        if radius >= 0 { b.cornerRadius = radius }
        b.addTarget(self, action: #selector(pressed), for: .touchUpInside)
        box.button = b
    }

    @objc private func pressed() { ctx.emit("press") }
}

final class RyUIAppleSignInBox: UIView {
    var schemeChanged: (() -> Void)?
    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        if previous?.userInterfaceStyle != traitCollection.userInterfaceStyle { schemeChanged?() }
    }
    var button: ASAuthorizationAppleIDButton? {
        didSet {
            oldValue?.removeFromSuperview()
            guard let b = button else { return }
            b.frame = bounds
            b.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            addSubview(b)
        }
    }
    /// As wide as it's given (at least Apple's 140), 44 tall.
    override func sizeThatFits(_ size: CGSize) -> CGSize {
        CGSize(width: max(min(size.width, 375), 140), height: 44)
    }
}
#endif
