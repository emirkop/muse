import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomRenderingLifecycleTests: XCTestCase {

    private func makeController(photoCount: Int, downloader: PhotoBytesDownloading? = nil) -> RealityKitSceneViewController {
        let content = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount, downloader: downloader)!
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        return controller
    }

    private func waitUntil(_ timeout: TimeInterval = 20, _ condition: @escaping @MainActor () -> Bool) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() && Date() < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
    }

    // MARK: - –27 behaviour intact

    func test_skeletonWithoutContent_isUnchanged() {
        let controller = RealityKitSceneViewController(content: nil)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertTrue(controller.isSceneLoaded)
        XCTAssertNil(controller.photoLayer, "no content, no photo layer")
        XCTAssertNotNil(controller.cameraController, " camera rig still attached")
        XCTAssertEqual(controller.movementController.subject, PlaceholderRoom.spawnPoint, " deterministic spawn intact")

        controller.viewDidDisappear(false)
        XCTAssertFalse(controller.isSceneLoaded)
    }

    func test_withContent_cameraMovementAndSpawnAreUnchanged() {
        let controller = makeController(photoCount: 5)
        controller.viewWillAppear(false)

        XCTAssertNotNil(controller.cameraController)
        XCTAssertEqual(controller.movementController.subject, PlaceholderRoom.spawnPoint)
        controller.viewDidDisappear(false)
    }

    // MARK: - Mount and load

    func test_appearing_mountsPlanesImmediately_thenTexturesArrive() async {
        let controller = makeController(photoCount: 4)

        controller.viewWillAppear(false)

        XCTAssertEqual(controller.photoLayer?.mountedSlotIndices, Set(0..<4))
        XCTAssertEqual(controller.texturedPhotoCount, 0)

        await waitUntil { controller.texturedPhotoCount == 4 }

        XCTAssertEqual(controller.texturedPhotoCount, 4)
        XCTAssertEqual(controller.failedPhotoCount, 0)
        XCTAssertEqual(controller.photoLayer?.texturedSlotIndices, Set(0..<4))
        controller.viewDidDisappear(false)
    }

    func test_everyVerificationCount_texturesAllPhotos() async {
        for count in [1, 2, 14, 27, 28] {
            let controller = makeController(photoCount: count)
            controller.viewWillAppear(false)
            XCTAssertEqual(controller.photoLayer?.mountedSlotIndices.count, count, "count \(count) planes")

            await waitUntil(40) { controller.texturedPhotoCount == count }

            XCTAssertEqual(controller.texturedPhotoCount, count, "count \(count) textured")
            XCTAssertEqual(controller.failedPhotoCount, 0, "count \(count) failures")
            let expectedAnchors = RoomPhotoSlotLayout.slots(forPhotoCount: count).map(\.anchor)
            XCTAssertEqual(controller.content?.placements.map(\.anchor), expectedAnchors, "count \(count) follows the focal→alternating→rear rule")
            controller.viewDidDisappear(false)
            XCTAssertNil(controller.photoLayer, "count \(count) torn down")
        }
    }

    // MARK: - Teardown and cancellation

    func test_disappearing_cancelsTheLoad_andNothingArrivesAfterwards() async {
        let slow = SlowFixtureDownloader(delayNanoseconds: 200_000_000)
        let controller = makeController(photoCount: 10, downloader: slow)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount >= 1 }
        let texturedAtExit = controller.texturedPhotoCount

        controller.viewDidDisappear(false)

        XCTAssertNil(controller.photoLayer, "layer released with the scene")
        XCTAssertFalse(controller.isSceneLoaded)
        try? await Task.sleep(nanoseconds: 600_000_000)
        XCTAssertEqual(controller.texturedPhotoCount, texturedAtExit, "no texture may be applied after teardown")
        XCTAssertLessThan(slow.completed, 10, "remaining downloads were cancelled, not drained")
    }

    func test_replacingContentMidLoad_dropsThePreviousRoomsResults() async {
        let slow = SlowFixtureDownloader(delayNanoseconds: 150_000_000)
        let controller = makeController(photoCount: 10, downloader: slow)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount >= 1 }

        controller.replaceContent(RoomRenderingVerificationFixture.makeContent(photoCount: 2))
        XCTAssertEqual(controller.texturedPhotoCount, 0, "counters reset for the new Room")
        XCTAssertEqual(controller.photoLayer?.mountedSlotIndices, [0, 1])

        await waitUntil { controller.texturedPhotoCount == 2 }
        try? await Task.sleep(nanoseconds: 400_000_000)

        XCTAssertEqual(controller.texturedPhotoCount, 2, "exactly the new Room's photographs, none of the old")
        XCTAssertEqual(controller.photoLayer?.texturedSlotIndices, [0, 1])
        XCTAssertEqual(controller.photoLayer?.root.children.count, 2)
        controller.viewDidDisappear(false)
    }

    func test_replacingWithNil_returnsToTheSkeleton() async {
        let controller = makeController(photoCount: 3)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 3 }

        controller.replaceContent(nil)

        XCTAssertNil(controller.photoLayer)
        XCTAssertNil(controller.content)
        XCTAssertTrue(controller.isSceneLoaded, "the environment stays; only the photographs go")
        controller.viewDidDisappear(false)
    }

    // MARK: - Failures

    func test_individualFailures_leaveTheRoomAndOtherPhotosIntact() async {
        let failing = FailingFixtureDownloader(failSlots: [1, 3])
        let controller = makeController(photoCount: 5, downloader: failing)
        controller.viewWillAppear(false)

        await waitUntil { controller.texturedPhotoCount + controller.failedPhotoCount == 5 }

        XCTAssertEqual(controller.texturedPhotoCount, 3)
        XCTAssertEqual(controller.failedPhotoCount, 2)
        XCTAssertEqual(controller.photoLayer?.texturedSlotIndices, [0, 2, 4])
        XCTAssertEqual(controller.photoLayer?.mountedSlotIndices.count, 5, "failed slots keep their plane; the Room stands")
        XCTAssertTrue(controller.isSceneLoaded)
        controller.viewDidDisappear(false)
    }

    // MARK: - Repeated entry

    func test_repeatedEnterAndLeave_leavesNoResidue() async {
        weak var lastLayer: RoomPhotoLayer?
        for cycle in 0..<6 {
            let controller = makeController(photoCount: 7)
            controller.viewWillAppear(false)
            await waitUntil { controller.texturedPhotoCount == 7 }
            lastLayer = controller.photoLayer
            controller.viewDidDisappear(false)
            XCTAssertNil(controller.photoLayer, "cycle \(cycle)")
            XCTAssertFalse(controller.isSceneLoaded, "cycle \(cycle)")
        }
        XCTAssertNil(lastLayer, "a torn-down photo layer must be deallocated, not retained by a lingering task")
    }
}

// MARK: - Fixture variants for lifecycle tests

final class SlowFixtureDownloader: PhotoBytesDownloading, @unchecked Sendable {
    private let inner = RoomRenderingVerificationFixture.FixturePhotoDownloader()
    private let delayNanoseconds: UInt64
    private let lock = NSLock()
    private var _completed = 0
    var completed: Int { lock.withLock { _completed } }

    init(delayNanoseconds: UInt64) { self.delayNanoseconds = delayNanoseconds }

    func download(_ url: URL) async throws -> Data {
        try await Task.sleep(nanoseconds: delayNanoseconds)
        let data = try await inner.download(url)
        lock.withLock { _completed += 1 }
        return data
    }
}

final class FailingFixtureDownloader: PhotoBytesDownloading, @unchecked Sendable {
    private let inner = RoomRenderingVerificationFixture.FixturePhotoDownloader()
    private let failSlots: Set<Int>

    init(failSlots: Set<Int>) { self.failSlots = failSlots }

    func download(_ url: URL) async throws -> Data {
        if let slot = Int(url.lastPathComponent), failSlots.contains(slot) {
            throw PhotoDownloadError.transport
        }
        return try await inner.download(url)
    }
}
