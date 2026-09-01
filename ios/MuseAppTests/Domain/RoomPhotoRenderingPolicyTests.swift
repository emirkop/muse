import simd
import XCTest
@testable import MuseApp

final class RoomPhotoTexturePolicyTests: XCTestCase {

    func test_worstCaseFootprint_neverExceedsTheBudget_atAnyCount() {
        for count in 1...Room.maxPhotos {
            XCTAssertLessThanOrEqual(
                RoomPhotoTexturePolicy.worstCaseRoomBytes(forPhotoCount: count),
                RoomPhotoTexturePolicy.textureBudgetBytes,
                "count \(count) breaks the \(RoomPhotoTexturePolicy.textureBudgetBytes / 1_048_576) MB ceiling"
            )
        }
    }

    func test_tiering_isDeterministicAndMonotonic() {
        XCTAssertEqual(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 1), 1_024)
        XCTAssertEqual(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 14), 1_024)
        XCTAssertEqual(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 18), 1_024)
        XCTAssertEqual(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 19), 768)
        XCTAssertEqual(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 27), 768)
        XCTAssertEqual(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 28), 768)

        var previous = Int.max
        for count in 1...Room.maxPhotos {
            let edge = RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: count)
            XCTAssertLessThanOrEqual(edge, previous, "more photos must never mean larger textures")
            XCTAssertTrue(RoomPhotoTexturePolicy.candidateLongEdges.contains(edge))
            previous = edge
        }
    }

    func test_runtimeTextures_areAlwaysSmallerThanTheStoredSource() {
        for count in 1...Room.maxPhotos {
            XCTAssertLessThan(RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: count), NormalizedPhotoFile.maxLongEdge)
        }
    }

    func test_worstCaseBytes_matchTheDocumentedTable() {
        XCTAssertEqual(RoomPhotoTexturePolicy.worstCaseBytes(longEdge: 1_024), Int(Double(1_024 * 1_024 * 4) * 4.0 / 3.0))
        XCTAssertEqual(RoomPhotoTexturePolicy.worstCaseBytes(longEdge: 768) / 1_048_576, 3)
        XCTAssertEqual(RoomPhotoTexturePolicy.worstCaseRoomBytes(forPhotoCount: 28) / 1_048_576, 84)
    }

    func test_concurrencyBound_isSmall() {
        XCTAssertEqual(RoomPhotoTexturePolicy.maxConcurrentLoads, 3)
    }
}

final class PhotoMountSizingTests: XCTestCase {
    private func envelope(_ w: Float, _ h: Float) -> SlotTransform {
        SlotTransform(position: .zero, scale: SIMD3<Float>(w, h, 1))
    }

    func test_landscapePhotoInLandscapeEnvelope_isWidthLimited() {
        let size = PhotoMountSizing.planeSize(envelope: envelope(1.6, 1.2), pixelWidth: 3072, pixelHeight: 2048)
        XCTAssertEqual(size.width, 1.6, accuracy: 0.0001)
        XCTAssertEqual(size.height, 1.6 * 2048 / 3072, accuracy: 0.0001)
    }

    func test_portraitPhotoInLandscapeEnvelope_isHeightLimited() {
        let size = PhotoMountSizing.planeSize(envelope: envelope(1.6, 1.2), pixelWidth: 2048, pixelHeight: 3072)
        XCTAssertEqual(size.height, 1.2, accuracy: 0.0001)
        XCTAssertEqual(size.width, 1.2 * 2048 / 3072, accuracy: 0.0001)
    }

    func test_squarePhotoInSquareEnvelope_fillsIt() {
        let size = PhotoMountSizing.planeSize(envelope: envelope(0.62, 0.62), pixelWidth: 2400, pixelHeight: 2400)
        XCTAssertEqual(size.width, 0.62, accuracy: 0.0001)
        XCTAssertEqual(size.height, 0.62, accuracy: 0.0001)
    }

    func test_aspectRatioIsAlwaysPreserved() {
        for (w, h) in [(3072, 2048), (2048, 3072), (2400, 2400), (3072, 1000), (1000, 3072)] {
            for env in [envelope(1.6, 1.2), envelope(0.62, 0.62), envelope(0.5, 2.0)] {
                let size = PhotoMountSizing.planeSize(envelope: env, pixelWidth: w, pixelHeight: h)
                XCTAssertEqual(size.width / size.height, Float(w) / Float(h), accuracy: 0.001, "\(w)x\(h) in \(env.scale)")
                XCTAssertLessThanOrEqual(size.width, env.scale.x + 0.0001, "must fit inside the envelope")
                XCTAssertLessThanOrEqual(size.height, env.scale.y + 0.0001, "must fit inside the envelope")
            }
        }
    }

    func test_degenerateInputs_yieldZero_notNaN() {
        for (w, h) in [(0, 100), (100, 0), (-1, 100)] {
            let size = PhotoMountSizing.planeSize(envelope: envelope(1, 1), pixelWidth: w, pixelHeight: h)
            XCTAssertEqual(size, .init(width: 0, height: 0))
        }
        let zeroEnvelope = PhotoMountSizing.planeSize(envelope: envelope(0, 1), pixelWidth: 100, pixelHeight: 100)
        XCTAssertEqual(zeroEnvelope, .init(width: 0, height: 0))
    }
}
