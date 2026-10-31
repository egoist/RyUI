#if canImport(UIKit)
import UIKit
import Security
// Each its own framework, loaded at launch if linked: only in an app that
// calls them (see the CLI's iosFeatures).
#if RYUI_PHOTOS
import PhotosUI
import UniformTypeIdentifiers
#endif
#if RYUI_BROWSER
import SafariServices
#endif
#if RYUI_WEBAUTH
import AuthenticationServices
#endif
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A native module's method: the call's bytes, and a reply (once).
public typealias RyUIModuleMethod = (_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) -> Void

struct RyUIModuleError: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

@MainActor extension RyUIRuntime {
    /// Registers a module's methods for ui.Call(module, method, ...): an
    /// app's (from its RyUIAppNatives.register), alongside the host's own.
    public func registerModule(_ name: String, _ methods: [String: RyUIModuleMethod]) {
        modules[name] = methods
    }

    /// Runs a Call command and answers with a Result event.
    func call(_ root: UInt32, _ args: UnsafeRawBufferPointer) {
        guard args.count >= 12 else { return }
        var r = RyUIReader(args)
        let id = r.u32()
        let module = r.string(), method = r.string()
        let payload = Data(bytes: r.p, count: r.end - r.p)
        if !builtinModules {
            builtinModules = true
            registerBuiltinModules()
        }
        var answered = false
        let reply: (Result<Data, Error>) -> Void = { [weak self] res in
            DispatchQueue.main.async {
                guard let self, !answered else { return }
                answered = true
                var out = [UInt8]()
                withUnsafeBytes(of: id.littleEndian) { out.append(contentsOf: $0) }
                switch res {
                case .success(let d): out.append(1); out.append(contentsOf: d)
                case .failure(let e): out.append(0); out.append(contentsOf: Array(e.localizedDescription.utf8))
                }
                let bytes = out
                // An answer, not an event the root listens for: sent as is.
                self.send(RyUIEvents.result(root, bytes))
            }
        }
        guard let m = modules[module]?[method] else {
            reply(.failure(RyUIModuleError("no module method \(module).\(method)")))
            return
        }
        m(payload, reply)
    }

    // The host's modules: secureStore, browser, photos, device, share.

    func registerBuiltinModules() {
        // An app's module of the same name stays the app's.
        let apps = modules
        defer { for (k, v) in apps { modules[k] = v } }
        // The app's ID, also in its share sheet (the extension's Info.plist
        // has it), whose keychain group is the app's.
        let service = Bundle.main.object(forInfoDictionaryKey: "RyUIAppID") as? String ?? Bundle.main.bundleIdentifier ?? "ryui"
        func query(_ key: String) -> [String: Any] {
            [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: key]
        }
        // Strings are u32-length prefixed, as ops strings.
        func split(_ d: Data) -> (String, Data) {
            guard d.count >= 4 else { return ("", Data()) }
            let n = Int(d.prefix(4).withUnsafeBytes { $0.loadUnaligned(as: UInt32.self).littleEndian })
            let key = String(decoding: d.dropFirst(4).prefix(n), as: UTF8.self)
            return (key, Data(d.dropFirst(4 + n)))
        }
        modules["secureStore"] = [
            // args: key; result: u8 found, then the value.
            "get": { args, reply in
                var q = query(String(decoding: args, as: UTF8.self))
                q[kSecReturnData as String] = true
                q[kSecMatchLimit as String] = kSecMatchLimitOne
                var out: CFTypeRef?
                let st = SecItemCopyMatching(q as CFDictionary, &out)
                if st == errSecItemNotFound { reply(.success(Data([0]))); return }
                guard st == errSecSuccess, let d = out as? Data else { reply(.failure(RyUIModuleError("keychain: \(st)"))); return }
                reply(.success(Data([1]) + d))
            },
            // args: string key, then the value.
            "set": { args, reply in
                let (key, value) = split(args)
                SecItemDelete(query(key) as CFDictionary)
                var q = query(key)
                q[kSecValueData as String] = value
                q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
                let st = SecItemAdd(q as CFDictionary, nil)
                reply(st == errSecSuccess ? .success(Data()) : .failure(RyUIModuleError("keychain: \(st)")))
            },
            "delete": { args, reply in
                let st = SecItemDelete(query(String(decoding: args, as: UTF8.self)) as CFDictionary)
                reply(st == errSecSuccess || st == errSecItemNotFound ? .success(Data()) : .failure(RyUIModuleError("keychain: \(st)")))
            },
        ]
        #if RYUI_BROWSER
        modules["browser"] = [
            // args: the URL: shown in the app (SFSafariViewController).
            "open": { [weak self] args, reply in
                guard let url = URL(string: String(decoding: args, as: UTF8.self)), url.scheme == "https" || url.scheme == "http" else {
                    reply(.failure(RyUIModuleError("not a web URL"))); return
                }
                guard let top = self?.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                top.present(SFSafariViewController(url: url), animated: true)
                reply(.success(Data()))
            },
        ]
        #endif
        #if RYUI_WEBAUTH
        modules["webAuth"] = [
            // args: u32 session, u8 flags (1 ephemeral), string URL, string
            // callback scheme (empty: none); result: the callback URL, or
            // the error "canceled".
            "start": { [weak self] args, reply in
                guard args.count >= 13 else { reply(.failure(RyUIModuleError("bad args"))); return }
                let (id, flags, link, scheme) = args.withUnsafeBytes { b -> (UInt32, UInt8, String, String) in
                    var r = RyUIReader(b)
                    return (r.u32(), r.u8(), r.string(), r.string())
                }
                let url = URL(string: link)
                guard let url, url.scheme == "https" || url.scheme == "http" else { reply(.failure(RyUIModuleError("not a web URL"))); return }
                guard let self, let window = self.topController?.view.window else { reply(.failure(RyUIModuleError("no screen"))); return }
                let s = ASWebAuthenticationSession(url: url, callbackURLScheme: scheme.isEmpty ? nil : scheme) { [weak self] callback, error in
                    DispatchQueue.main.async {
                        self?.authSessions[id] = nil
                        if let callback { reply(.success(Data(callback.absoluteString.utf8))); return }
                        if let e = error as? ASWebAuthenticationSessionError, e.code == .canceledLogin {
                            reply(.failure(RyUIModuleError("canceled"))); return
                        }
                        reply(.failure(error ?? RyUIModuleError("canceled")))
                    }
                }
                let anchor = RyUIAuthAnchor(window)
                s.presentationContextProvider = anchor
                s.prefersEphemeralWebBrowserSession = flags & 1 != 0
                self.authSessions[id] = (s, anchor, reply)
                if !s.start() {
                    self.authSessions[id] = nil
                    reply(.failure(RyUIModuleError("couldn't start the sign-in")))
                }
            },
            // args: u32 session: closes it; its start answers "canceled".
            "cancel": { [weak self] args, reply in
                guard args.count >= 4 else { reply(.success(Data())); return }
                let id = args.withUnsafeBytes { b -> UInt32 in var r = RyUIReader(b); return r.u32() }
                if let (s, _, start) = self?.authSessions.removeValue(forKey: id) {
                    s.cancel()
                    start(.failure(RyUIModuleError("canceled")))
                }
                reply(.success(Data()))
            },
        ]
        #endif
        #if RYUI_PHOTOS
        modules["photos"] = [
            // args: u8 limit (0: one); result: the picked images' files, a
            // line each (none: cancelled).
            "pick": { [weak self] args, reply in
                guard let self, let top = self.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                var config = PHPickerConfiguration()
                config.filter = .images
                config.selectionLimit = max(Int(args.first ?? 1), 1)
                let p = PHPickerViewController(configuration: config)
                let d = RyUIPhotoPicker(reply)
                p.delegate = d
                objc_setAssociatedObject(p, &RyUIPhotoPicker.key, d, .OBJC_ASSOCIATION_RETAIN)
                top.present(p, animated: true)
            },
        ]
        #endif
        #if RYUI_LOCATION
        modules["location"] = RyUILocation.shared.methods()
        #endif
        #if RYUI_NETWORK
        modules["network"] = RyUINetwork.shared.methods()
        #endif
        #if RYUI_LIBRARY
        modules["library"] = RyUILibrary.methods()
        #endif
        modules["share"] = RyUIShare.methods(self)
        modules["screen"] = RyUIScreen.shared.methods(self)
        modules["splash"] = RyUISplash.shared.methods()
        modules["clipboard"] = RyUIClipboard.shared.methods()
        #if RYUI_REVIEW
        modules["review"] = RyUIReview.methods(self)
        #endif
        #if RYUI_TRACKING
        modules["tracking"] = RyUITracking.methods()
        #endif
        #if RYUI_BIOMETRIC
        modules["biometric"] = RyUIBiometric.methods()
        #endif
        #if RYUI_WIDGET
        modules["widget"] = RyUIWidgetData.methods()
        #endif
        #if RYUI_CONTACTS
        modules["contacts"] = RyUIContacts.shared.methods(self)
        #endif
        #if RYUI_CALENDAR
        modules["calendar"] = RyUICalendar.shared.methods(self)
        #endif
        #if RYUI_SENSORS
        modules["sensors"] = RyUISensors.shared.methods()
        #endif
        #if RYUI_SPEECH
        modules["speech"] = RyUISpeech.shared.methods()
        #endif
        #if RYUI_IMAGEEDIT
        modules["imageEdit"] = RyUIImageEdit.methods()
        #endif
        #if RYUI_PERIODIC
        modules["periodic"] = RyUIPeriodic.shared.methods()
        #endif
        #if RYUI_INTEGRITY
        modules["integrity"] = RyUIIntegrity.methods()
        #endif
        #if RYUI_AGERANGE
        modules["ageRange"] = RyUIAgeRange.methods(self)
        #endif
        #if RYUI_AUDIORECORD
        modules["audioRecord"] = RyUIAudioRecord.shared.methods()
        #endif
        #if RYUI_PUSH
        modules["push"] = RyUIPush.shared.methods()
        #endif
        #if RYUI_APPLEAUTH
        modules["appleAuth"] = RyUIAppleAuth.shared.methods(self)
        #endif
        #if RYUI_FILES
        modules["files"] = RyUIFiles.methods(self)
        #endif
        #if RYUI_WEBVIEW
        modules["js"] = RyUIScript.methods()
        modules["web"] = RyUIWebView.methods()
        #endif
        #if RYUI_NOTIFY
        modules["notify"] = RyUINotify.shared.methods()
        #endif
        #if RYUI_AUDIO
        modules["audio"] = RyUIAudio.shared.methods()
        #endif
        modules["camera"] = RyUICamera.shared.methods()
        modules["background"] = RyUIBackground.shared.methods()
        #if RYUI_SCAN
        modules["scan"] = RyUIScan.methods()
        #endif
        modules["device"] = [
            // result: JSON: model, system, systemVersion, appVersion, build.
            "info": { args, reply in
                let info = Bundle.main.infoDictionary ?? [:]
                let d: [String: String] = [
                    "model": UIDevice.current.model,
                    "system": UIDevice.current.systemName,
                    "systemVersion": UIDevice.current.systemVersion,
                    "appVersion": info["CFBundleShortVersionString"] as? String ?? "",
                    "build": info["CFBundleVersion"] as? String ?? "",
                ]
                reply(.success((try? JSONSerialization.data(withJSONObject: d)) ?? Data("{}".utf8)))
            },
            // result: u8 has, then the clipboard's text (iOS asks the user
            // the first time an app reads what another put there).
            "clipboard": { _, reply in
                guard let s = UIPasteboard.general.string else { reply(.success(Data([0]))); return }
                reply(.success(Data([1]) + Data(s.utf8)))
            },
            // args: u8 on: the screen stays on (the idle timer off) while on.
            "keepAwake": { args, reply in
                UIApplication.shared.isIdleTimerDisabled = args.first == 1
                reply(.success(Data()))
            },
        ]
    }
}

#if RYUI_WEBAUTH
/// Where a sign-in's sheet is shown: the app's window.
final class RyUIAuthAnchor: NSObject, ASWebAuthenticationPresentationContextProviding {
    let window: UIWindow
    init(_ w: UIWindow) { window = w }
    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor { window }
}
#endif

#if RYUI_PHOTOS
/// Copies the picked images to files (the picker's are temporary).
final class RyUIPhotoPicker: NSObject, PHPickerViewControllerDelegate {
    nonisolated(unsafe) static var key = 0
    let reply: (Result<Data, Error>) -> Void
    init(_ r: @escaping (Result<Data, Error>) -> Void) { reply = r }

    func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
        picker.dismiss(animated: true)
        let group = DispatchGroup()
        var paths = [String](repeating: "", count: results.count)
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("ryui-photos", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        for (i, r) in results.enumerated() {
            group.enter()
            r.itemProvider.loadFileRepresentation(forTypeIdentifier: UTType.image.identifier) { url, _ in
                defer { group.leave() }
                guard let url else { return }
                let to = dir.appendingPathComponent(UUID().uuidString + "." + (url.pathExtension.isEmpty ? "jpg" : url.pathExtension))
                if (try? FileManager.default.copyItem(at: url, to: to)) != nil { paths[i] = to.path }
            }
        }
        let reply = self.reply
        group.notify(queue: .main) {
            reply(.success(Data(paths.filter { !$0.isEmpty }.joined(separator: "\n").utf8)))
        }
    }
}
#endif
#endif
