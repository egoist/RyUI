import UIKit

// The app around the RyUI host: one window scene showing the core's root.

// RyUIAppDelegate: the name an app with a share sheet's main starts it by
// (its code is a framework, Frameworks/RyUIApp.framework).
@main
@objc(RyUIAppDelegate)
final class AppDelegate: UIResponder, UIApplicationDelegate {
    override init() {
        super.init()
        // ryui.json's shortcuts: their App Intents hand the host their URL.
        NotificationCenter.default.addObserver(forName: Notification.Name("RyUIOpenURL"), object: nil, queue: .main) { n in
            guard let s = n.userInfo?["url"] as? String, let u = URL(string: s) else { return }
            MainActor.assumeIsolated { RyUIRuntime.shared.openURL(u) }
        }
    }

    #if RYUI_PERIODIC
    // ryui/periodic: its tasks' handlers, which the system wants before launch ends.
    func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        RyUIPeriodic.shared.registerAtLaunch()
        return true
    }
    #endif

    func application(_ application: UIApplication, configurationForConnecting session: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let c = UISceneConfiguration(name: nil, sessionRole: session.role)
        c.delegateClass = SceneDelegate.self
        return c
    }

    #if RYUI_PUSH
    // ryui/push: APNs' token, and data messages.
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken token: Data) {
        RyUIPush.shared.registered(token)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        RyUIPush.shared.failed(error)
    }

    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any],
                     fetchCompletionHandler done: @escaping (UIBackgroundFetchResult) -> Void) {
        // A notification with an alert comes through the center's delegate.
        if (userInfo["aps"] as? [String: Any])?["alert"] == nil { RyUIPush.shared.received(userInfo) }
        done(.newData)
    }
    #endif

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
