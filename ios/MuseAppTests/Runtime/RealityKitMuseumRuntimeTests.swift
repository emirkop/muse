import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RealityKitMuseumRuntimeTests: XCTestCase {
    func test_conformsToMuseumRuntimeInterface_andProducesAViewController() {
        let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime()

        let viewController = runtime.makeRuntimeViewController()

        XCTAssertTrue(viewController is RealityKitSceneViewController,
                      "the runtime must return a real RealityKit host, not the placeholder")
    }

    func test_appearing_attachesARenderingSurface() throws {
        let viewController = try XCTUnwrap(RealityKitMuseumRuntime().makeRuntimeViewController() as? RealityKitSceneViewController)

        viewController.loadViewIfNeeded()
        XCTAssertFalse(viewController.isSceneLoaded, "the scene is built on appearance, not merely on view load")

        viewController.viewWillAppear(false)

        XCTAssertTrue(viewController.isSceneLoaded, "the scene must be constructed once the screen appears")
        XCTAssertNotNil(renderingSurface(in: viewController), "a rendering surface must be attached to the view hierarchy")
    }

    func test_inRealWindow_renderingSurfaceGetsNonZeroBounds() throws {
        let viewController = try XCTUnwrap(RealityKitMuseumRuntime().makeRuntimeViewController() as? RealityKitSceneViewController)

        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 390, height: 844))
        window.rootViewController = viewController
        window.makeKeyAndVisible()
        viewController.viewWillAppear(false)
        window.layoutIfNeeded()

        let surface = try XCTUnwrap(renderingSurface(in: viewController))
        XCTAssertGreaterThan(surface.bounds.width, 0, "the rendering surface must be laid out with real width")
        XCTAssertGreaterThan(surface.bounds.height, 0, "the rendering surface must be laid out with real height")
        XCTAssertEqual(surface.bounds.size, viewController.view.bounds.size, "the rendering surface must fill its container")
    }

    private func renderingSurface(in viewController: UIViewController) -> ARView? {
        viewController.view.subviews.compactMap { $0 as? ARView }.first
    }

    func test_disappearing_tearsDownScene() throws {
        let viewController = try XCTUnwrap(RealityKitMuseumRuntime().makeRuntimeViewController() as? RealityKitSceneViewController)

        viewController.loadViewIfNeeded()
        viewController.viewWillAppear(false)
        XCTAssertTrue(viewController.isSceneLoaded)

        viewController.viewDidDisappear(false)

        XCTAssertFalse(viewController.isSceneLoaded, "leaving the screen must tear the scene down, not leave it resident")
        XCTAssertNil(renderingSurface(in: viewController), "the rendering surface must be detached from the view hierarchy")
        XCTAssertNil(viewController.cameraController, "the camera rig must be released with the scene")
    }

    func test_reappearing_rebuildsScene() throws {
        let viewController = try XCTUnwrap(RealityKitMuseumRuntime().makeRuntimeViewController() as? RealityKitSceneViewController)

        viewController.loadViewIfNeeded()
        viewController.viewWillAppear(false)
        viewController.viewDidDisappear(false)
        XCTAssertFalse(viewController.isSceneLoaded)

        viewController.viewWillAppear(false)

        XCTAssertTrue(viewController.isSceneLoaded, "returning to the screen must rebuild the scene it released")
    }

    func test_repeatedPushAndPop_deallocatesEveryController() {
        let navigationController = UINavigationController(rootViewController: UIViewController())
        var weakControllers: [() -> RealityKitSceneViewController?] = []

        for _ in 0..<5 {
            autoreleasepool {
                let viewController = RealityKitMuseumRuntime().makeRuntimeViewController() as! RealityKitSceneViewController
                weakControllers.append { [weak viewController] in viewController }

                navigationController.pushViewController(viewController, animated: false)
                viewController.loadViewIfNeeded()
                viewController.viewWillAppear(false)
                navigationController.popViewController(animated: false)
                viewController.viewDidDisappear(false)
            }
        }

        for (index, weakController) in weakControllers.enumerated() {
            XCTAssertNil(weakController(), "scene controller \(index) leaked after push/pop")
        }
    }
}
