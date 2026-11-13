#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif
#if canImport(CAsuka)
@_implementationOnly import CAsuka
#endif

/// The share extension of an app with a share sheet of its own
/// (app.ShareSheet): its principal class, which runs the app's Go code
/// in the extension, with the sheet as its root (ASUKA_SHARE_SHEET). iOS
/// shows it as a sheet over the app that shares. What's shared is
/// collected (AsukaShareCollector: files into the App Group) and sent to
/// the root as the Shared event; the root's ShareClose ends it.
@objc(AsukaShareSheetController)
public final class AsukaShareSheetController: UIViewController {
    private var collector: AsukaShareCollector?
    private var items: [AsukaShareCollector.Item] = []
    private var collected = false

    public override func viewDidLoad() {
        super.viewDidLoad()
        "ASUKA_SHARE_SHEET".withCString { k in "1".withCString { v in asuka_setenv(k, v) } }
        let rt = AsukaRuntime.shared
        rt.sheet = self
        let vc = AsukaViewController()
        addChild(vc)
        vc.view.frame = view.bounds
        vc.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(vc.view)
        vc.didMove(toParent: self)
        // The sheet goes on a swipe down only as the screen says (Close).
        isModalInPresentation = true
        let c = AsukaShareCollector(extensionContext)
        collector = c
        c.collect { [weak self] items in
            guard let self else { return }
            self.items = items
            self.collected = true
            self.deliver()
        }
    }

    /// Sends what's shared to the root, once there's both.
    func deliver() {
        let rt = AsukaRuntime.shared
        guard collected, rt.started, rt.rootID != 0, let c = collector else { return }
        var w = AsukaWriter()
        w.u32(UInt32(items.count))
        for it in items {
            w.u8(UInt8(clamping: it.kind))
            w.string(it.text)
            w.string(c.absolute(it))
            w.string(it.name)
            w.string(it.mime)
            w.u64(UInt64(bitPattern: it.size))
        }
        collected = false // once
        rt.send(AsukaEvents.shared(rt.rootID, w.bytes))
    }

    /// The root's ShareClose: 0 done, 1 cancelled, 2 open the app with
    /// what's shared (its files stay in the App Group for it).
    func close(_ how: UInt8) {
        guard let ctx = extensionContext, let c = collector else { return }
        collector = nil
        if how == 2, let url = c.shareURL(items) {
            AsukaShareCollector.openApp(url, from: self) { _ in ctx.completeRequest(returningItems: nil) }
            return
        }
        switch how {
        case 1:
            c.remove(items)
            ctx.cancelRequest(withError: NSError(domain: NSCocoaErrorDomain, code: NSUserCancelledError))
        default:
            c.remove(items)
            ctx.completeRequest(returningItems: nil)
        }
    }
}

/// The directory the app keeps its data in (store.Dir) when it has a
/// share sheet or a push rewrite, which run in its extensions: in the App
/// Group they share, so all see the same; nil without either (the app's
/// container). Go asks for it as its packages initialize, before the
/// host has started it.
@_cdecl("asuka_ios_shared_dir")
public func asukaIOSSharedDir() -> UnsafeMutablePointer<CChar>? {
    let info = Bundle.main.infoDictionary ?? [:]
    guard info["AsukaShareSheet"] as? Bool == true || info["AsukaNotificationService"] as? Bool == true,
          let group = Bundle.main.object(forInfoDictionaryKey: "AsukaAppGroup") as? String,
          let c = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else { return nil }
    return strdup(c.appendingPathComponent("Library/Application Support", isDirectory: true).path)
}
#endif
