import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class SceneEnvironmentLightingSnapshotTests: XCTestCase {

    private var repositoryRoot: URL {
        URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
    }

    private static let views: [(name: String, eye: SIMD3<Float>, yaw: Float, pitch: Float)] = [
        ("01_entry", SIMD3(0, 1.60, 8.20), 0, -1),
        ("02_nave", SIMD3(1.30, 1.60, 4.90), -12, 0),
        ("03_focal", SIMD3(0, 1.60, -1.60), 0, 21),
        ("04_vault", SIMD3(0, 1.60, 1.00), 0, 56),
        ("05_arcade", SIMD3(1.05, 1.62, 3.20), 58, 5),
        ("07_panel", SIMD3(2.30, 1.55, 0.55), 84, 6)
    ]

    func test_renderTheAuthoredRoomThroughTheRealRenderer() throws {
        guard ProcessInfo.processInfo.environment["MUSE_LIGHTING_SNAPSHOT"] == "1" else {
            throw XCTSkip("set MUSE_LIGHTING_SNAPSHOT=1 to render review frames")
        }
        let geometry = repositoryRoot
            .appendingPathComponent("build/bundles/bundle_style_gothic_Hall/v2/geometry.usdz")
        guard FileManager.default.fileExists(atPath: geometry.path) else {
            throw XCTSkip("the published Gothic Hall bundle is not reachable from this test host")
        }
        let outputDirectory = repositoryRoot
            .appendingPathComponent("build/review/gothic_hall_v3_runtime")
        try? FileManager.default.createDirectory(
            at: outputDirectory, withIntermediateDirectories: true)

        let arView = ARView(frame: CGRect(x: 0, y: 0, width: 1200, height: 675),
                            cameraMode: .nonAR,
                            automaticallyConfigureSession: false)
        let window = UIWindow(frame: arView.frame)
        window.addSubview(arView)
        window.makeKeyAndVisible()

        SceneEnvironmentLighting.apply(to: arView)

        let anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
        let room = try Entity.load(contentsOf: geometry)
        anchor.addChild(room)

        let camera = PerspectiveCamera()
        camera.camera.fieldOfViewInDegrees = 55
        anchor.addChild(camera)

        var written: [String] = []
        for view in Self.views {
            camera.position = view.eye
            camera.orientation = simd_quatf(angle: view.yaw * .pi / 180, axis: SIMD3(0, 1, 0))
                * simd_quatf(angle: view.pitch * .pi / 180, axis: SIMD3(1, 0, 0))

            let expectation = expectation(description: "snapshot \(view.name)")
            var captured: UIImage?
            arView.snapshot(saveToHDR: false) { image in
                captured = image
                expectation.fulfill()
            }
            wait(for: [expectation], timeout: 30)

            guard let image = captured, let png = image.pngData() else {
                XCTFail("the renderer returned no frame for \(view.name)")
                continue
            }
            let url = outputDirectory.appendingPathComponent("runtime_\(view.name).png")
            try png.write(to: url)
            written.append(url.lastPathComponent)

            XCTAssertGreaterThan(dynamicRange(of: image), 0.05,
                                 "\(view.name) rendered as a flat field, not a lit room")
        }
        XCTAssertFalse(written.isEmpty)
        print("runtime review frames: \(outputDirectory.path)\n  \(written.joined(separator: "\n  "))")
    }

    private func dynamicRange(of image: UIImage) -> Double {
        guard let cgImage = image.cgImage else { return 0 }
        let width = 48, height = 27
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        guard let space = CGColorSpace(name: CGColorSpace.sRGB),
              let context = CGContext(
                data: &pixels, width: width, height: height,
                bitsPerComponent: 8, bytesPerRow: width * 4, space: space,
                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return 0 }
        context.draw(cgImage, in: CGRect(x: 0, y: 0, width: width, height: height))
        var low = 1.0, high = 0.0
        for index in stride(from: 0, to: pixels.count, by: 4) {
            let luma = (0.2126 * Double(pixels[index])
                        + 0.7152 * Double(pixels[index + 1])
                        + 0.0722 * Double(pixels[index + 2])) / 255.0
            low = min(low, luma)
            high = max(high, luma)
        }
        return high - low
    }
}
