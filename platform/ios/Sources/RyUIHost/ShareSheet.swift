#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif
#if canImport(CRyUI)
import CRyUI
#endif

/// The share extension of an app with a share sheet of its own
/// (app.ShareSheet): its principal class, which runs the app's Go code
/// in the extension, with the sheet as its root (RYUI_SHARE_SHEET). iOS
/// shows it as a sheet over the app that shares. What's shared is
/// collected (RyUIShareCollector: files into the App Group) and sent to
/// the root as the Shared event; the root's ShareClose ends it.
@objc(RyUIShareSheetController)
public final class RyUIShareSheetController: UIViewController {
    private var collector: RyUIShareCollector?
    private var items: [RyUIShareCollector.Item] = []
    private var collected = false

    public override func viewDidLoad() {
        super.viewDidLoad()
        "RYUI_SHARE_SHEET".withCString { k in "1".withCString { v in ryui_setenv(k, v) } }
        let rt = RyUIRuntime.shared
        rt.sheet = self
        let vc = RyUIViewController()
        addChild(vc)
        vc.view.frame = view.bounds
        vc.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(vc.view)
        vc.didMove(toParent: self)
        // The sheet goes on a swipe down only as the screen says (Close).
        isModalInPresentation = true
        let c = RyUIShareCollector(extensionContext)
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
        let rt = RyUIRuntime.shared
        guard collected, rt.started, rt.rootID != 0, let c = collector else { return }
        var w = RyUIWriter()
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
        rt.send(RyUIEvents.shared(rt.rootID, w.bytes))
    }

    /// The root's ShareClose: 0 done, 1 cancelled, 2 open the app with
    /// what's shared (its files stay in the App Group for it).
    func close(_ how: UInt8) {
        guard let ctx = extensionContext, let c = collector else { return }
        collector = nil
        if how == 2, let url = c.shareURL(items) {
            RyUIShareCollector.openApp(url, from: self) { _ in ctx.completeRequest(returningItems: nil) }
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
/// share sheet, which runs in its share extension: in the App Group the
/// two share, so both see the same; nil without a sheet (the app's
/// container). Go asks for it as its packages initialize, before the
/// host has started it.
@_cdecl("ryui_ios_shared_dir")
public func ryuiIOSSharedDir() -> UnsafeMutablePointer<CChar>? {
    guard Bundle.main.object(forInfoDictionaryKey: "RyUIShareSheet") as? Bool == true,
          let group = Bundle.main.object(forInfoDictionaryKey: "RyUIShareGroup") as? String,
          let c = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else { return nil }
    return strdup(c.appendingPathComponent("Library/Application Support", isDirectory: true).path)
}
#endif
