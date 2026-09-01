import CoreGraphics
import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class SceneEnvironmentLightingTests: XCTestCase {

    // MARK: - Helpers

    private func luminance(elevation: Double, azimuth: Double) -> Double {
        let c = SceneEnvironmentLighting.radiance(elevation: elevation, azimuth: azimuth)
        return 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b
    }

    private func ringMean(elevation: Double, samples: Int = 64) -> Double {
        var total = 0.0
        for i in 0..<samples {
            total += luminance(elevation: elevation,
                               azimuth: Double(i) / Double(samples) * 2 * .pi)
        }
        return total / Double(samples)
    }

    // MARK: - The map

    func test_theMapIsEquirectangularAndTheDeclaredSize() throws {
        let image = try XCTUnwrap(SceneEnvironmentLighting.makeEquirectangularImage())
        XCTAssertEqual(image.width, SceneEnvironmentLighting.mapWidth)
        XCTAssertEqual(image.height, SceneEnvironmentLighting.mapHeight)
        XCTAssertEqual(image.width, image.height * 2)
    }

    func test_theEnvironmentIsBrighterAboveThanBelow() {
        let above = ringMean(elevation: .pi / 3)
        let below = ringMean(elevation: -.pi / 3)
        XCTAssertGreaterThan(above, below * 1.8,
                             "the zenith must be materially brighter than the nadir")
    }

    func test_theGlazedStoreyBandIsTheBrightestPartOfTheSky() {
        let band = ringMean(elevation: 0.30)
        XCTAssertGreaterThan(band, ringMean(elevation: .pi / 2 - 0.05))
        XCTAssertGreaterThan(band, ringMean(elevation: -0.30))
    }

    func test_theMapHasNoSeamWhereTheAzimuthWraps() {
        let epsilon = 1e-6
        for elevationStep in stride(from: -1.4, through: 1.4, by: 0.35) {
            let before = luminance(elevation: elevationStep, azimuth: 2 * .pi - epsilon)
            let after = luminance(elevation: elevationStep, azimuth: epsilon)
            XCTAssertEqual(before, after, accuracy: 1e-4,
                           "seam at elevation \(elevationStep)")
        }
    }

    func test_azimuthalVariationExistsAndIsFourFold() {
        let samples = (0..<24).map {
            luminance(elevation: 0.30, azimuth: Double($0) / 24 * 2 * .pi)
        }
        let spread = (samples.max() ?? 0) - (samples.min() ?? 0)
        XCTAssertGreaterThan(spread, 0.01, "the ring carries no azimuthal variation")

        for i in 0..<12 {
            let azimuth = Double(i) / 12 * 2 * .pi
            XCTAssertEqual(luminance(elevation: 0.30, azimuth: azimuth),
                           luminance(elevation: 0.30, azimuth: azimuth + .pi / 2),
                           accuracy: 1e-6,
                           "not invariant under a 90-degree rotation")
        }
    }

    func test_theMapIsFullyOpaqueAndWithinRange() throws {
        let image = try XCTUnwrap(SceneEnvironmentLighting.makeEquirectangularImage())
        XCTAssertEqual(image.bitsPerComponent, 8)
        XCTAssertEqual(image.colorSpace?.name, CGColorSpace.sRGB)
        for elevation in stride(from: -1.5, through: 1.5, by: 0.25) {
            let c = SceneEnvironmentLighting.radiance(elevation: elevation, azimuth: 0.7)
            for channel in [c.r, c.g, c.b] {
                XCTAssertGreaterThan(channel, 0.0)
                XCTAssertLessThan(channel, 1.0, "a channel clips at elevation \(elevation)")
            }
        }
    }

    // MARK: - The resource

    func test_realityKitAcceptsTheGeneratedEnvironment() throws {
        let resource = SceneEnvironmentLighting.resource()
        XCTAssertNotNil(resource, "RealityKit refused the generated environment")
    }

    func test_theResourceIsBuiltOnceAndReused() throws {
        let first = try XCTUnwrap(SceneEnvironmentLighting.resource())
        let second = try XCTUnwrap(SceneEnvironmentLighting.resource())
        XCTAssertTrue(first === second)
    }

    // MARK: - Application

    func test_applyingItSetsBothTheResourceAndTheExposure() throws {
        let arView = ARView(frame: CGRect(x: 0, y: 0, width: 64, height: 64),
                            cameraMode: .nonAR,
                            automaticallyConfigureSession: false)
        XCTAssertNil(arView.environment.lighting.resource,
                     "a bare ARView is expected to start unlit — that was the bug")

        SceneEnvironmentLighting.apply(to: arView)

        XCTAssertNotNil(arView.environment.lighting.resource)
        XCTAssertEqual(arView.environment.lighting.intensityExponent,
                       SceneEnvironmentLighting.intensityExponent)
    }

    func test_aRoomSceneIsLitOnceItLoads() {
        let controller = RealityKitSceneViewController(content: nil)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertTrue(controller.isSceneLoaded)
        XCTAssertTrue(controller.hasEnvironmentLightingForTesting,
                      "the Room scene mounted geometry with no light source")
    }
}
