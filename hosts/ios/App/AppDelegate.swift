import UIKit

// The app around the RyUI host: one window scene showing the core's root.

@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
    func application(_ application: UIApplication, configurationForConnecting session: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let c = UISceneConfiguration(name: nil, sessionRole: session.role)
        c.delegateClass = SceneDelegate.self
        return c
    }

    func applicationDidReceiveMemoryWarning(_ application: UIApplication) {
        RyUIRuntime.shared.lifecycle(.memoryWarning)
    }
}

final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options: UIScene.ConnectionOptions) {
        guard let ws = scene as? UIWindowScene else { return }
        let w = UIWindow(windowScene: ws)
        w.rootViewController = RyUIViewController()
        w.makeKeyAndVisible()
        window = w
        // Opened with a URL, or a universal link.
        for c in options.urlContexts { RyUIRuntime.shared.openURL(c.url) }
        for a in options.userActivities where a.activityType == NSUserActivityTypeBrowsingWeb {
            if let u = a.webpageURL { RyUIRuntime.shared.openURL(u) }
        }
    }

    func scene(_ scene: UIScene, openURLContexts contexts: Set<UIOpenURLContext>) {
        for c in contexts { RyUIRuntime.shared.openURL(c.url) }
    }

    func scene(_ scene: UIScene, continue activity: NSUserActivity) {
        if activity.activityType == NSUserActivityTypeBrowsingWeb, let u = activity.webpageURL {
            RyUIRuntime.shared.openURL(u)
        }
    }

    func sceneWillEnterForeground(_ scene: UIScene) { RyUIRuntime.shared.lifecycle(.foreground) }
    func sceneDidEnterBackground(_ scene: UIScene) { RyUIRuntime.shared.lifecycle(.background) }
}
