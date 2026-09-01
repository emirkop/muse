import UIKit

final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?
    private var appCoordinator: AppCoordinator?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options: UIScene.ConnectionOptions) {
        guard let windowScene = scene as? UIWindowScene else { return }

        let window = UIWindow(windowScene: windowScene)
        self.window = window

        let coordinator = AppCoordinator(window: window)
        appCoordinator = coordinator
        coordinator.start()

        for activity in options.userActivities {
            handle(activity, with: coordinator)
        }
    }

    func scene(_ scene: UIScene, continue userActivity: NSUserActivity) {
        guard let appCoordinator else { return }
        handle(userActivity, with: appCoordinator)
    }

    private func handle(_ activity: NSUserActivity, with coordinator: AppCoordinator) {
        guard activity.activityType == NSUserActivityTypeBrowsingWeb, let url = activity.webpageURL else { return }
        coordinator.handleIncomingURL(url)
    }
}
