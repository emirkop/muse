import XCTest
@testable import MuseApp

@MainActor
final class PhotoSelectionViewModelTests: XCTestCase {

    private func makeRoom(photoCount: Int) -> Room {
        Room(
            id: "room_1",
            name: "Trabzon",
            variantID: "variant_a",
            privacy: .private,
            photoSlots: (0..<photoCount).map {
                PhotoSlotAssignment(slotIndex: $0, photoAssetID: "existing_\($0)", caption: "")
            }
        )
    }

    private func makeViewModel(room: Room, uploader: FakeRoomPhotoUploader = FakeRoomPhotoUploader()) -> PhotoSelectionViewModel {
        PhotoSelectionViewModel(room: room, uploader: uploader, accessToken: "token")
    }

    private func makePhotos(_ count: Int, failingAt failures: Set<Int> = []) -> [PickedPhoto] {
        (0..<count).map { index in
            PickedPhoto(
                id: "picked_\(index)",
                assetIdentifier: nil,
                loadState: failures.contains(index) ? .failed : .ready(thumbnail: Data([0x01]), file: Self.stubFile(index))
            )
        }
    }

    private static func stubFile(_ index: Int) -> NormalizedPhotoFile {
        NormalizedPhotoFile(
            fileURL: URL(fileURLWithPath: "/tmp/stub_\(index).jpg"),
            contentType: "image/jpeg", byteSize: 1024, pixelWidth: 1200, pixelHeight: 800,
            sha256Hex: String(repeating: "a", count: 64)
        )
    }

    // MARK: - The 28-photo cap

    func test_remainingCapacity_isCorrectAtEveryPhotoCount() {
        for existing in 0...Room.maxPhotos {
            let viewModel = makeViewModel(room: makeRoom(photoCount: existing))

            XCTAssertEqual(viewModel.remainingCapacity, Room.maxPhotos - existing, "existing=\(existing)")
            XCTAssertEqual(viewModel.selectionLimit, Room.maxPhotos - existing, "existing=\(existing)")
            XCTAssertEqual(viewModel.isRoomFull, existing == Room.maxPhotos, "existing=\(existing)")
        }
    }

    func test_fullRoom_startsInRoomFull_andNeverOpensThePicker() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: Room.maxPhotos))

        XCTAssertEqual(viewModel.state, .roomFull)

        viewModel.beginSelection()

        XCTAssertEqual(viewModel.state, .roomFull, "a full Room must not enter selection")
    }

    func test_selectionLimitOfZero_isOnlyEverReachableInTheRoomFullState() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: Room.maxPhotos))

        XCTAssertEqual(viewModel.selectionLimit, 0)
        XCTAssertEqual(viewModel.state, .roomFull)
        XCTAssertTrue(viewModel.isRoomFull)
    }

    func test_overSizedSelection_isClampedToRemainingCapacity() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 26))

        viewModel.ingest(makePhotos(10))

        XCTAssertEqual(viewModel.selectedPhotos.count, 2, "only 2 slots remain")
        XCTAssertEqual(viewModel.projectedPhotoCount, Room.maxPhotos)
    }

    func test_fillingTheRoomExactly_isAllowed_andLeavesNoCapacity() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(Room.maxPhotos))

        XCTAssertEqual(viewModel.selectedPhotos.count, Room.maxPhotos)
        XCTAssertEqual(viewModel.projectedPhotoCount, Room.maxPhotos)
        XCTAssertEqual(viewModel.projectedLayout.count, Room.maxPhotos)
    }

    func test_projectedCountNeverExceedsTheCap() {
        for existing in 0...Room.maxPhotos {
            let viewModel = makeViewModel(room: makeRoom(photoCount: existing))

            viewModel.ingest(makePhotos(Room.maxPhotos + 5))

            XCTAssertLessThanOrEqual(
                viewModel.projectedPhotoCount, Room.maxPhotos,
                "existing=\(existing) must never project beyond the cap"
            )
        }
    }

    // MARK: - Cancellation and empty selection

    func test_cancellation_returnsToReady_ratherThanStranding() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 3))
        viewModel.beginSelection()
        XCTAssertEqual(viewModel.state, .selecting)

        viewModel.ingest([])

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertTrue(viewModel.selectedPhotos.isEmpty)
    }

    func test_cancellationFromAFullRoom_returnsToRoomFull() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: Room.maxPhotos))

        viewModel.ingest([])

        XCTAssertEqual(viewModel.state, .roomFull)
    }

    func test_reset_discardsTheSelection() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 1))
        viewModel.ingest(makePhotos(3))
        XCTAssertEqual(viewModel.selectedPhotos.count, 3)

        viewModel.reset()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertTrue(viewModel.selectedPhotos.isEmpty)
        XCTAssertEqual(viewModel.projectedPhotoCount, 1, "the Room's own photos are untouched")
    }

    // MARK: - Ordering and slot assignment (engine)

    func test_newPhotos_takeTheSlotsAfterTheExistingOnes_inPickedOrder() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 2))

        viewModel.ingest(makePhotos(3))

        let assignments = viewModel.newPhotoAssignments
        XCTAssertEqual(assignments.map(\.photo.id), ["picked_0", "picked_1", "picked_2"])
        XCTAssertEqual(assignments.map(\.slot.index), [2, 3, 4])
    }

    func test_assignmentMatchesThePlacementEnginesLayout_atEveryCount() {
        for existing in 0..<Room.maxPhotos {
            let viewModel = makeViewModel(room: makeRoom(photoCount: existing))
            viewModel.ingest(makePhotos(1))

            let expected = RoomPhotoSlotLayout.slots(forPhotoCount: existing + 1)
            XCTAssertEqual(viewModel.projectedLayout, expected, "existing=\(existing)")
            XCTAssertEqual(
                viewModel.newPhotoAssignments.first?.slot, expected.last,
                "existing=\(existing): the new photo takes the final placement position"
            )
        }
    }

    func test_reflow_isDetectedAndSurfaced_whenAddingChangesExistingWalls() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 4))

        viewModel.ingest(makePhotos(1))

        XCTAssertTrue(viewModel.existingPhotosWillReflow)
        XCTAssertNotNil(viewModel.reflowNotice)
        XCTAssertEqual(RoomPhotoSlotLayout.slots(forPhotoCount: 4)[3].wall, .rear)
        XCTAssertEqual(RoomPhotoSlotLayout.slots(forPhotoCount: 5)[3].wall, .left)
    }

    func test_noReflowReported_whenTheRoomWasEmpty() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(6))

        XCTAssertFalse(viewModel.existingPhotosWillReflow)
        XCTAssertNil(viewModel.reflowNotice)
    }

    func test_assignmentsCarryWallRolesOnly_neverCoordinates() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(3))

        XCTAssertEqual(viewModel.newPhotoAssignments.map(\.slot.wall), [.focal, .left, .right])
    }

    // MARK: - Per-photo load failure

    func test_aFailedPhoto_isReportedWithoutDiscardingTheOthers() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(4, failingAt: [1]))

        XCTAssertEqual(viewModel.selectedPhotos.count, 4, "the failure must not drop any photo")
        XCTAssertEqual(viewModel.failedPhotoCount, 1)
        XCTAssertNotNil(viewModel.failureNotice)
        XCTAssertEqual(viewModel.selectedPhotos.filter(\.didLoad).count, 3)
    }

    func test_failedPhotosStillHoldTheirPlacementPosition() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(3, failingAt: [0]))

        XCTAssertEqual(viewModel.newPhotoAssignments.map(\.slot.index), [0, 1, 2])
    }

    // MARK: - Commit

    func test_commit_convergesOnTheServersSlotList_andClearsTheSelection() async {
        let uploader = FakeRoomPhotoUploader()
        uploader.outcome = PhotoUploadOutcome(
            photoSlots: (0..<5).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "") },
            failures: []
        )
        let viewModel = makeViewModel(room: makeRoom(photoCount: 2), uploader: uploader)
        viewModel.ingest(makePhotos(3))

        await viewModel.commit()

        guard case .committed(let outcome) = viewModel.state else {
            return XCTFail("expected .committed, got \(viewModel.state)")
        }
        XCTAssertEqual(outcome.photoSlots.count, 5)
        XCTAssertEqual(viewModel.existingPhotoCount, 5)
        XCTAssertEqual(viewModel.remainingCapacity, Room.maxPhotos - 5)
        XCTAssertTrue(viewModel.selectedPhotos.isEmpty, "a fully committed selection has nothing left to retry")
        XCTAssertEqual(uploader.executed.first?.roomID, "room_1")
        XCTAssertEqual(uploader.executed.first?.photos.map(\.id), ["picked_0", "picked_1", "picked_2"])
        XCTAssertEqual(viewModel.committedMessage(outcome), "Added 3 photographs to Trabzon.")
    }

    func test_commit_withPartialFailure_keepsOnlyTheFailedPhotosForRetry() async {
        let uploader = FakeRoomPhotoUploader()
        uploader.outcome = PhotoUploadOutcome(
            photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_0", caption: ""),
                         PhotoSlotAssignment(slotIndex: 1, photoAssetID: "asset_2", caption: "")],
            failures: [.init(pickedPhotoID: "picked_1", reason: .transferFailed)]
        )
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0), uploader: uploader)
        viewModel.ingest(makePhotos(3))

        await viewModel.commit()

        guard case .committed(let outcome) = viewModel.state else {
            return XCTFail("expected .committed, got \(viewModel.state)")
        }
        XCTAssertTrue(outcome.hasFailures)
        XCTAssertEqual(viewModel.selectedPhotos.map(\.id), ["picked_1"])
        XCTAssertEqual(viewModel.existingPhotoCount, 2)
        XCTAssertEqual(viewModel.retryActionTitle, "Retry 1 Photo")
        XCTAssertEqual(viewModel.committedMessage(outcome), "Added 2 photographs to Trabzon. 1 photograph couldn't be saved.")
    }

    func test_retry_recommitsOnlyTheFailedPhotos_withTheSameIDs() async {
        let uploader = FakeRoomPhotoUploader()
        uploader.outcome = PhotoUploadOutcome(
            photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_0", caption: "")],
            failures: [.init(pickedPhotoID: "picked_1", reason: .transport)]
        )
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0), uploader: uploader)
        viewModel.ingest(makePhotos(2))
        await viewModel.commit()

        uploader.outcome = PhotoUploadOutcome(
            photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_0", caption: ""),
                         PhotoSlotAssignment(slotIndex: 1, photoAssetID: "asset_1", caption: "")],
            failures: []
        )
        await viewModel.retry()

        XCTAssertEqual(uploader.executed.count, 2)
        XCTAssertEqual(uploader.executed[1].photos.map(\.id), ["picked_1"], "retry must send only the failed photo")
        guard case .committed(let outcome) = viewModel.state else {
            return XCTFail("expected .committed, got \(viewModel.state)")
        }
        XCTAssertFalse(outcome.hasFailures)
        XCTAssertTrue(viewModel.selectedPhotos.isEmpty)
    }

    func test_commit_batchFailure_retainsTheWholeSelection() async {
        let uploader = FakeRoomPhotoUploader()
        uploader.error = IdentityAPIClientError.transport
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0), uploader: uploader)
        viewModel.ingest(makePhotos(3))

        await viewModel.commit()

        XCTAssertEqual(viewModel.state, .commitFailed)
        XCTAssertEqual(viewModel.selectedPhotos.count, 3)
        XCTAssertEqual(viewModel.existingPhotoCount, 0, "nothing was saved, so nothing changes")

        uploader.error = nil
        uploader.outcome = PhotoUploadOutcome(
            photoSlots: (0..<3).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") },
            failures: []
        )
        await viewModel.retry()
        guard case .committed = viewModel.state else {
            return XCTFail("retry after a batch failure must be able to succeed; got \(viewModel.state)")
        }
    }

    func test_commit_reportsProgress_andNeverRegressesAfterCompletion() async {
        let uploader = FakeRoomPhotoUploader()
        uploader.progressSteps = [(1, 3), (2, 3), (3, 3)]
        uploader.outcome = PhotoUploadOutcome(
            photoSlots: (0..<3).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") },
            failures: []
        )
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0), uploader: uploader)
        var observed: [PhotoSelectionViewModel.State] = []
        viewModel.onStateChange = { observed.append($0) }
        viewModel.ingest(makePhotos(3))

        await viewModel.commit()
        await Task.yield()
        await Task.yield()

        XCTAssertTrue(observed.contains(.uploading(completed: 0, total: 3)))
        guard case .committed = viewModel.state else {
            return XCTFail("a late progress callback must not regress a committed state; got \(viewModel.state)")
        }
    }

    func test_commit_withoutASelection_isANoOp() async {
        let uploader = FakeRoomPhotoUploader()
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0), uploader: uploader)

        await viewModel.commit()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertTrue(uploader.executed.isEmpty)
    }

    // MARK: - Copy

    func test_confirmActionTitle_isCountAware() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(1))
        XCTAssertEqual(viewModel.confirmActionTitle, "Add 1 Photo")

        viewModel.reset()
        viewModel.ingest(makePhotos(14))
        XCTAssertEqual(viewModel.confirmActionTitle, "Add 14 Photos")
    }

    func test_counterText_reportsTheProjectedRoomTotalAgainstTheCap() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 6))

        viewModel.ingest(makePhotos(8))

        XCTAssertEqual(viewModel.counterText, "14 / 28 photos in this Room")
    }

    func test_pickedPhotosCarryNoAssetIdentifier_underTheNoPermissionPicker() {
        let viewModel = makeViewModel(room: makeRoom(photoCount: 0))

        viewModel.ingest(makePhotos(2))

        for photo in viewModel.selectedPhotos {
            XCTAssertNil(photo.assetIdentifier)
        }
    }
}

final class FakeRoomPhotoUploader: RoomPhotoUploading, @unchecked Sendable {
    struct Call {
        let roomID: String
        let photos: [PickedPhoto]
    }

    var outcome = PhotoUploadOutcome(photoSlots: [], failures: [])
    var error: Error?
    var progressSteps: [(Int, Int)] = []
    private(set) var executed: [Call] = []

    func execute(
        accessToken: String,
        roomID: String,
        photos: [PickedPhoto],
        onProgress: UploadRoomPhotosUseCase.ProgressHandler?
    ) async throws -> PhotoUploadOutcome {
        executed.append(Call(roomID: roomID, photos: photos))
        for (completed, total) in progressSteps {
            onProgress?(completed, total)
        }
        if let error { throw error }
        return outcome
    }
}
