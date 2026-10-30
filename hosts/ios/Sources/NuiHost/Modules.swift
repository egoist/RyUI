#if canImport(UIKit)
import UIKit
import Security
// Each its own framework, loaded at launch if linked: only in an app that
// calls them (see the CLI's iosFeatures).
#if NUI_PHOTOS
import PhotosUI
import UniformTypeIdentifiers
#endif
#if NUI_BROWSER
import SafariServices
#endif
#if canImport(NuiOps)
import NuiOps
#endif

/// A native module's method: the call's bytes, and a reply (once).
public typealias NuiModuleMethod = (_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) -> Void

struct NuiModuleError: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

@MainActor extension NuiRuntime {
    /// Registers a module's methods for ui.Call(module, method, ...): an
    /// app's (from its NuiAppNatives.register), alongside the host's own.
    public func registerModule(_ name: String, _ methods: [String: NuiModuleMethod]) {
        modules[name] = methods
    }

    /// Runs a Call command and answers with a Result event.
    func call(_ root: UInt32, _ args: UnsafeRawBufferPointer) {
        guard args.count >= 12 else { return }
        var r = NuiReader(args)
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
                self.send(NuiEvents.result(root, bytes))
            }
        }
        guard let m = modules[module]?[method] else {
            reply(.failure(NuiModuleError("no module method \(module).\(method)")))
            return
        }
        m(payload, reply)
    }

    // The host's modules: secureStore, browser, photos, device.

    func registerBuiltinModules() {
        // An app's module of the same name stays the app's.
        let apps = modules
        defer { for (k, v) in apps { modules[k] = v } }
        let service = Bundle.main.bundleIdentifier ?? "nui"
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
                guard st == errSecSuccess, let d = out as? Data else { reply(.failure(NuiModuleError("keychain: \(st)"))); return }
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
                reply(st == errSecSuccess ? .success(Data()) : .failure(NuiModuleError("keychain: \(st)")))
            },
            "delete": { args, reply in
                let st = SecItemDelete(query(String(decoding: args, as: UTF8.self)) as CFDictionary)
                reply(st == errSecSuccess || st == errSecItemNotFound ? .success(Data()) : .failure(NuiModuleError("keychain: \(st)")))
            },
        ]
        #if NUI_BROWSER
        modules["browser"] = [
            // args: the URL: shown in the app (SFSafariViewController).
            "open": { [weak self] args, reply in
                guard let url = URL(string: String(decoding: args, as: UTF8.self)), url.scheme == "https" || url.scheme == "http" else {
                    reply(.failure(NuiModuleError("not a web URL"))); return
                }
                guard let top = self?.topController else { reply(.failure(NuiModuleError("no screen"))); return }
                top.present(SFSafariViewController(url: url), animated: true)
                reply(.success(Data()))
            },
        ]
        #endif
        #if NUI_PHOTOS
        modules["photos"] = [
            // args: u8 limit (0: one); result: the picked images' files, a
            // line each (none: cancelled).
            "pick": { [weak self] args, reply in
                guard let self, let top = self.topController else { reply(.failure(NuiModuleError("no screen"))); return }
                var config = PHPickerConfiguration()
                config.filter = .images
                config.selectionLimit = max(Int(args.first ?? 1), 1)
                let p = PHPickerViewController(configuration: config)
                let d = NuiPhotoPicker(reply)
                p.delegate = d
                objc_setAssociatedObject(p, &NuiPhotoPicker.key, d, .OBJC_ASSOCIATION_RETAIN)
                top.present(p, animated: true)
            },
        ]
        #endif
        #if NUI_LOCATION
        modules["location"] = NuiLocation.shared.methods()
        #endif
        #if NUI_NOTIFY
        modules["notify"] = NuiNotify.shared.methods()
        #endif
        #if NUI_AUDIO
        modules["audio"] = NuiAudio.shared.methods()
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

#if NUI_PHOTOS
/// Copies the picked images to files (the picker's are temporary).
final class NuiPhotoPicker: NSObject, PHPickerViewControllerDelegate {
    nonisolated(unsafe) static var key = 0
    let reply: (Result<Data, Error>) -> Void
    init(_ r: @escaping (Result<Data, Error>) -> Void) { reply = r }

    func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
        picker.dismiss(animated: true)
        let group = DispatchGroup()
        var paths = [String](repeating: "", count: results.count)
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("nui-photos", isDirectory: true)
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
