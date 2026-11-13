import UIKit

// The share extension (asuka.json's shareTarget) of an app with no share
// sheet of its own: what another app shares from its share sheet goes to
// the app, which it opens, as Android's ACTION_SEND opens the app;
// ui.OnShare gets it. It runs no Go: it collects the items
// (AsukaShareCollector, in the host's sources) and hands them over in a
// URL of the app's share scheme (Info.plist's AsukaShareScheme,
// asukashare.<app ID>), which the app's host turns into the Shared event.
// Texts and links are in the URL; files are in the App Group the two
// share, which the app moves them out of. An app with a sheet
// (app.ShareSheet) has AsukaShareSheetController instead, with its Go.

@objc(AsukaShareViewController)
final class ShareViewController: UIViewController {
    private var started = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .clear
    }

    // Once on screen: opening the app goes through the responder chain,
    // which reaches the application only from a view in a window.
    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        guard !started else { return }
        started = true
        let c = AsukaShareCollector(extensionContext)
        c.collect { items in
            guard !items.isEmpty, let url = c.shareURL(items) else { return self.finish() }
            AsukaShareCollector.openApp(url, from: self) { _ in self.finish() }
        }
    }

    private func finish() {
        extensionContext?.completeRequest(returningItems: nil)
    }
}
