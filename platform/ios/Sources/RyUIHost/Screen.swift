#if canImport(UIKit)
import UIKit

/// ryui/screen's module: "lock" (u8 orientation), "bars" (u8 style, u8
/// flags: 1 status bar hidden), "preventCapture" (u8 on), and, while
/// watching, "screenshot" (answered at the next screenshot) and
/// "stopScreenshots". The root and the screens' controllers read the
/// orientations and the status bar from here.
@MainActor final class RyUIScreen: NSObject {
    static let shared = RyUIScreen()
    /// The app's lock (screen.Lock); nil: Info.plist's orientations.
    static var lock: UIInterfaceOrientationMask?
    static var statusBarStyle: UIStatusBarStyle = .default
    static var statusBarHidden = false

    private var preventing = false
    private var cover: UIView?
    private var screenshots: [(Result<Data, Error>) -> Void] = []
    private var observing = false

    func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            // As screen.Orientation: any, portrait, portraitUp, landscape,
            // landscapeLeft, landscapeRight.
            "lock": { [weak rt] args, reply in
                let iPad = UIDevice.current.userInterfaceIdiom == .pad
                let masks: [UIInterfaceOrientationMask?] = [nil, iPad ? [.portrait, .portraitUpsideDown] : .portrait, .portrait,
                                                            .landscape, .landscapeLeft, .landscapeRight]
                let o = Int(args.first ?? 0)
                var mask = o < masks.count ? masks[o] : nil
                // Only what Info.plist allows: UIKit throws for a lock with none of it.
                if let m = mask, let w = rt?.controller?.view.window {
                    let allowed = UIApplication.shared.supportedInterfaceOrientations(for: w)
                    mask = m.intersection(allowed).isEmpty ? nil : m.intersection(allowed)
                }
                Self.lock = mask
                if let c = rt?.controller {
                    c.setNeedsUpdateOfSupportedInterfaceOrientations()
                    if let m = mask { c.view.window?.windowScene?.requestGeometryUpdate(.iOS(interfaceOrientations: m)) }
                }
                reply(.success(Data()))
            },
            "bars": { [weak rt] args, reply in
                let style = args.first ?? 0
                Self.statusBarStyle = style == 1 ? .lightContent : style == 2 ? .darkContent : .default
                Self.statusBarHidden = args.count > 1 && args[1] & 1 != 0
                var vc: UIViewController? = rt?.controller
                while let c = vc {
                    UIView.animate(withDuration: 0.25) { c.setNeedsStatusBarAppearanceUpdate() }
                    vc = c.presentedViewController
                }
                reply(.success(Data()))
            },
            "preventCapture": { [weak self, weak rt] args, reply in
                guard let self else { return }
                self.preventing = args.first == 1
                self.observe()
                self.captureChanged(rt)
                reply(.success(Data()))
            },
            "screenshot": { [weak self] _, reply in
                guard let self else { return }
                self.observe()
                self.screenshots.append(reply)
            },
            "stopScreenshots": { [weak self] _, reply in
                let ws = self?.screenshots ?? []
                self?.screenshots = []
                for w in ws { w(.failure(RyUIModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }

    private func observe() {
        guard !observing else { return }
        observing = true
        let nc = NotificationCenter.default
        nc.addObserver(self, selector: #selector(screenshotTaken), name: UIApplication.userDidTakeScreenshotNotification, object: nil)
        nc.addObserver(self, selector: #selector(capturedChanged), name: UIScreen.capturedDidChangeNotification, object: nil)
    }

    @objc private func screenshotTaken() {
        let ws = screenshots
        screenshots = []
        for w in ws { w(.success(Data())) }
    }

    @objc private func capturedChanged() { captureChanged(RyUIRuntime.shared) }

    /// Covers the window while it's recorded, mirrored or on AirPlay, if
    /// the app prevents capture.
    private func captureChanged(_ rt: RyUIRuntime?) {
        guard let window = rt?.controller?.view.window else { return }
        let captured = window.windowScene?.screen.isCaptured ?? false
        if preventing && captured {
            if cover == nil {
                let v = UIView(frame: window.bounds)
                v.autoresizingMask = [.flexibleWidth, .flexibleHeight]
                v.backgroundColor = .systemBackground
                cover = v
            }
            window.addSubview(cover!)
            window.bringSubviewToFront(cover!)
        } else {
            cover?.removeFromSuperview()
        }
    }
}
#endif
