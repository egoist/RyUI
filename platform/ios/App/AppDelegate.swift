import UIKit

// The app around the Asuka host: one window scene showing the core's root.

// AsukaAppDelegate: the name an app with a share sheet's main starts it by
// (its code is a framework, Frameworks/AsukaApp.framework).
@main
@objc(AsukaAppDelegate)
final class AppDelegate: UIResponder, UIApplicationDelegate {
    override init() {
        super.init()
        // asuka.json's shortcuts: their App Intents hand the host their URL.
        NotificationCenter.default.addObserver(forName: Notification.Name("AsukaOpenURL"), object: nil, queue: .main) { n in
            guard let s = n.userInfo?["url"] as? String, let u = URL(string: s) else { return }
            MainActor.assumeIsolated { AsukaRuntime.shared.openURL(u) }
        }
    }

    func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        #if ASUKA_PERIODIC
        // asuka/sdk/periodic: its tasks' handlers, which the system wants before launch ends.
        AsukaPeriodic.shared.registerAtLaunch()
        #endif
        #if ASUKA_LOCATION_BACKGROUND
        // asuka/sdk/location: the delegate for the events the system launched
        // the app for (a region, a significant change), and its updates again.
        AsukaLocationBackground.shared.registerAtLaunch()
        #endif
        #if ASUKA_NOTIFY || ASUKA_PUSH || ASUKA_NOTIFICATION_SERVICE
        // asuka/sdk/notify, asuka/sdk/push: the center's delegate before launch
        // ends, to see the response that launched the app (a background
        // action launches it without a scene).
        AsukaNotifyCenter.shared.install()
        #endif
        #if ASUKA_PURCHASE
        // asuka/sdk/purchase: StoreKit's transactions, listened to from the
        // start, so none that comes before the Go code observes is lost.
        AsukaStore.shared.start()
        #endif
        return true
    }

    func application(_ application: UIApplication, configurationForConnecting session: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let c = UISceneConfiguration(name: nil, sessionRole: session.role)
        c.delegateClass = SceneDelegate.self
        return c
    }

    #if ASUKA_PUSH
    // asuka/sdk/push: APNs' token, and data messages.
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken token: Data) {
        AsukaPush.shared.registered(token)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        AsukaPush.shared.failed(error)
    }

    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any],
                     fetchCompletionHandler done: @escaping (UIBackgroundFetchResult) -> Void) {
        // A notification with an alert comes through the center's delegate.
        if (userInfo["aps"] as? [String: Any])?["alert"] == nil { AsukaPush.shared.received(userInfo) }
        done(.newData)
    }
    #endif

    func applicationDidReceiveMemoryWarning(_ application: UIApplication) {
        AsukaRuntime.shared.lifecycle(.memoryWarning)
    }
}

final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options: UIScene.ConnectionOptions) {
        guard let ws = scene as? UIWindowScene else { return }
        let w = UIWindow(windowScene: ws)
        w.rootViewController = AsukaViewController()
        w.makeKeyAndVisible()
        window = w
        // Opened with a URL, or a universal link.
        AsukaRuntime.shared.openURLs(options.urlContexts.map { $0.url })
        for a in options.userActivities where a.activityType == NSUserActivityTypeBrowsingWeb {
            if let u = a.webpageURL { AsukaRuntime.shared.openURL(u) }
        }
    }

    func scene(_ scene: UIScene, openURLContexts contexts: Set<UIOpenURLContext>) {
        AsukaRuntime.shared.openURLs(contexts.map { $0.url })
    }

    func scene(_ scene: UIScene, continue activity: NSUserActivity) {
        if activity.activityType == NSUserActivityTypeBrowsingWeb, let u = activity.webpageURL {
            AsukaRuntime.shared.openURL(u)
        }
    }

    func sceneWillEnterForeground(_ scene: UIScene) { AsukaRuntime.shared.lifecycle(.foreground) }
    func sceneDidEnterBackground(_ scene: UIScene) { AsukaRuntime.shared.lifecycle(.background) }
}
