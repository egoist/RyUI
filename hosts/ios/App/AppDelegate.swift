import UIKit

// The app around the nui host: one window scene showing the core's root.

@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
    func application(_ application: UIApplication, configurationForConnecting session: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let c = UISceneConfiguration(name: nil, sessionRole: session.role)
        c.delegateClass = SceneDelegate.self
        return c
    }

    func applicationDidReceiveMemoryWarning(_ application: UIApplication) {
        NuiRuntime.shared.lifecycle(.memoryWarning)
    }
}

final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options: UIScene.ConnectionOptions) {
        guard let ws = scene as? UIWindowScene else { return }
        let w = UIWindow(windowScene: ws)
        w.rootViewController = NuiViewController()
        w.makeKeyAndVisible()
        window = w
    }

    func sceneWillEnterForeground(_ scene: UIScene) { NuiRuntime.shared.lifecycle(.foreground) }
    func sceneDidEnterBackground(_ scene: UIScene) { NuiRuntime.shared.lifecycle(.background) }
}
