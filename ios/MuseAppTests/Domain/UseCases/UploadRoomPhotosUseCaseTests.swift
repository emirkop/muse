import XCTest
@testable import MuseApp

final class UploadRoomPhotosUseCaseTests: XCTestCase {

    private var spoolDir: URL!

    override func setUpWithError() throws {
        spoolDir = FileManager.default.temporaryDirectory.appendingPathComponent("usecase-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: spoolDir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: spoolDir)
    }

    private func makePhoto(_ index: Int, normalized: Bool = true) throws -> PickedPhoto {
        let id = "picked_\(index)"
        guard normalized else {
            return PickedPhoto(id: id, assetIdentifier: nil, loadState: .failed)
        }
        let url = spoolDir.appendingPathComponent("\(id).jpg")
        try Data(repeating: UInt8(index), count: 64).write(to: url)
        let file = NormalizedPhotoFile(
            fileURL: url, contentType: "image/jpeg", byteSize: 64, pixelWidth: 1200, pixelHeight: 800,
            sha256Hex: String(repeating: String(format: "%x", index % 16), count: 64)
        )
        return PickedPhoto(id: id, assetIdentifier: nil, loadState: .ready(thumbnail: Data([1]), file: file))
    }

    private func makeUseCase(
        service: FakeRoomPhotoService,
        uploader: FakeObjectUploader,
        removed: Removed,
        maxConcurrent: Int = 3
    ) -> UploadRoomPhotosUseCase {
        UploadRoomPhotosUseCase(
            photoService: service,
            uploader: uploader,
            removeSpooledFile: { url in removed.record(url) },
            maxConcurrent: maxConcurrent
        )
    }

    // MARK: - Happy path

    func test_uploadsEveryPhoto_thenAssignsInPickedOrder_andCleansUp() async throws {
        let service = FakeRoomPhotoService()
        let uploader = FakeObjectUploader()
        let removed = Removed()
        let photos = try (0..<4).map { try makePhoto($0) }

        let outcome = try await makeUseCase(service: service, uploader: uploader, removed: removed)
            .execute(accessToken: "t", roomID: "room", photos: photos)

        let declarations = await service.declarations
        XCTAssertEqual(Set(declarations.map(\.clientUploadID)), Set(photos.map(\.id)))
        for declaration in declarations {
            XCTAssertEqual(declaration.contentType, "image/jpeg")
            XCTAssertEqual(declaration.byteSize, 64)
            XCTAssertEqual(declaration.pixelWidth, 1200)
        }
        let uploads = await uploader.uploads
        XCTAssertEqual(uploads.count, 4)
        for upload in uploads {
            XCTAssertEqual(upload.instructions.headers["Content-Type"], "image/jpeg")
        }
        let assigns = await service.assignCalls
        XCTAssertEqual(assigns.count, 1)
        XCTAssertEqual(assigns[0].assetIDs, ["asset_picked_0", "asset_picked_1", "asset_picked_2", "asset_picked_3"])
        XCTAssertEqual(assigns[0].roomID, "room")
        XCTAssertEqual(outcome.photoSlots.count, 4)
        XCTAssertFalse(outcome.hasFailures)
        XCTAssertEqual(Set(removed.urls), Set(photos.compactMap { $0.normalizedFile?.fileURL }))
    }

    func test_emptySelection_isANoOp() async throws {
        let service = FakeRoomPhotoService()
        let outcome = try await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
            .execute(accessToken: "t", roomID: "room", photos: [])
        XCTAssertEqual(outcome, PhotoUploadOutcome(photoSlots: [], failures: []))
        let declarations = await service.declarations
        XCTAssertTrue(declarations.isEmpty)
    }

    // MARK: - Concurrency bound

    func test_neverExceedsTheConcurrencyBound() async throws {
        let service = FakeRoomPhotoService()
        let uploader = FakeObjectUploader(delayNanoseconds: 20_000_000)
        let photos = try (0..<12).map { try makePhoto($0) }

        _ = try await makeUseCase(service: service, uploader: uploader, removed: Removed(), maxConcurrent: 3)
            .execute(accessToken: "t", roomID: "room", photos: photos)

        let peak = await uploader.peakInFlight
        XCTAssertGreaterThan(peak, 1, "uploads should actually overlap")
        XCTAssertLessThanOrEqual(peak, 3, "never more than the bound in flight")
    }

    // MARK: - Per-photograph failure isolation

    func test_oneTransferFailure_isReportedAlone_andTheRestAreAssigned() async throws {
        let service = FakeRoomPhotoService()
        let uploader = FakeObjectUploader()
        let removed = Removed()
        let photos = try (0..<3).map { try makePhoto($0) }
        await uploader.fail(pickedID: "picked_1")

        let outcome = try await makeUseCase(service: service, uploader: uploader, removed: removed)
            .execute(accessToken: "t", roomID: "room", photos: photos)

        XCTAssertEqual(outcome.failures, [.init(pickedPhotoID: "picked_1", reason: .transferFailed)])
        let assigns = await service.assignCalls
        XCTAssertEqual(assigns.first?.assetIDs, ["asset_picked_0", "asset_picked_2"])
        XCTAssertFalse(removed.urls.contains(photos[1].normalizedFile!.fileURL))
        XCTAssertEqual(removed.urls.count, 2)
    }

    func test_aPhotoThatNeverNormalized_isReportedWithoutBeingAttempted() async throws {
        let service = FakeRoomPhotoService()
        let photos = [try makePhoto(0), try makePhoto(1, normalized: false)]

        let outcome = try await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
            .execute(accessToken: "t", roomID: "room", photos: photos)

        XCTAssertEqual(outcome.failures, [.init(pickedPhotoID: "picked_1", reason: .notNormalized)])
        let declarations = await service.declarations
        XCTAssertEqual(declarations.map(\.clientUploadID), ["picked_0"], "nothing to declare for an unnormalized photo")
    }

    func test_aRejectedDeclaration_isReportedAlone() async throws {
        let service = FakeRoomPhotoService()
        await service.rejectDeclaration(clientUploadID: "picked_0", status: 400, message: "photo too large")
        let photos = try (0..<2).map { try makePhoto($0) }

        let outcome = try await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
            .execute(accessToken: "t", roomID: "room", photos: photos)

        XCTAssertEqual(outcome.failures, [.init(pickedPhotoID: "picked_0", reason: .rejectedDeclaration(message: "photo too large"))])
        let assigns = await service.assignCalls
        XCTAssertEqual(assigns.first?.assetIDs, ["asset_picked_1"])
    }

    func test_assetSpecificRefusalAtCommit_recomposesTheBatchWithoutIt() async throws {
        let service = FakeRoomPhotoService()
        await service.refuseAtCommit(assetID: "asset_picked_1", code: "asset_invalid", status: 422)
        let photos = try (0..<3).map { try makePhoto($0) }

        let outcome = try await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
            .execute(accessToken: "t", roomID: "room", photos: photos)

        let assigns = await service.assignCalls
        XCTAssertEqual(assigns.count, 2)
        XCTAssertEqual(assigns[0].assetIDs, ["asset_picked_0", "asset_picked_1", "asset_picked_2"])
        XCTAssertEqual(assigns[1].assetIDs, ["asset_picked_0", "asset_picked_2"])
        XCTAssertEqual(outcome.failures, [.init(pickedPhotoID: "picked_1", reason: .rejectedAtCommit(code: "asset_invalid"))])
        XCTAssertEqual(outcome.photoSlots.count, 2)
    }

    func test_batchLevelAssignFailure_propagates() async throws {
        let service = FakeRoomPhotoService()
        await service.failAssignBatch()
        let photos = try (0..<2).map { try makePhoto($0) }

        do {
            _ = try await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
                .execute(accessToken: "t", roomID: "room", photos: photos)
            XCTFail("expected the batch failure to propagate")
        } catch {
        }
    }

    // MARK: - Replacement: the same pipeline, a different last step

    func test_replacePhoto_uploadsThroughTheSamePipeline_thenReplaces_andCleansUp() async throws {
        let service = FakeRoomPhotoService()
        let uploader = FakeObjectUploader()
        let removed = Removed()
        let photo = try makePhoto(0)

        let outcome = await makeUseCase(service: service, uploader: uploader, removed: removed)
            .replacePhoto(accessToken: "t", roomID: "room", photoAssetID: "b", with: photo)

        let declarations = await service.declarations
        XCTAssertEqual(declarations.map(\.clientUploadID), ["picked_0"])
        let uploads = await uploader.uploads
        XCTAssertEqual(uploads.count, 1)
        XCTAssertEqual(uploads[0].file, photo.normalizedFile!.fileURL)
        let replaces = await service.replaceCalls
        XCTAssertEqual(replaces, [.init(roomID: "room", photoAssetID: "b", replacementAssetID: "asset_picked_0")])
        let assigns = await service.assignCalls
        XCTAssertTrue(assigns.isEmpty, "a replacement never appends")
        guard case .replaced(let slots, let newID) = outcome else { return XCTFail("expected .replaced, got \(outcome)") }
        XCTAssertEqual(newID, "asset_picked_0")
        XCTAssertEqual(slots.map(\.photoAssetID), ["a", "asset_picked_0", "c"])
        XCTAssertEqual(slots[1].caption, "kept caption", "")
        XCTAssertEqual(slots[1].slotIndex, 1)
        XCTAssertEqual(removed.urls, [photo.normalizedFile!.fileURL])
    }

    func test_replacePhoto_transferFailure_isReported_andKeepsTheSpoolFile() async throws {
        let service = FakeRoomPhotoService()
        let uploader = FakeObjectUploader()
        let removed = Removed()
        let photo = try makePhoto(0)
        await uploader.fail(pickedID: "picked_0")

        let outcome = await makeUseCase(service: service, uploader: uploader, removed: removed)
            .replacePhoto(accessToken: "t", roomID: "room", photoAssetID: "b", with: photo)

        XCTAssertEqual(outcome, .failed(.transferFailed))
        let replaces = await service.replaceCalls
        XCTAssertTrue(replaces.isEmpty, "nothing reaches the server when the bytes did not")
        XCTAssertTrue(removed.urls.isEmpty, "kept for an in-session retry")
    }

    func test_replacePhoto_serverRefusal_isRejectedAtCommitWithTheCode_andKeepsTheSpoolFile() async throws {
        let service = FakeRoomPhotoService()
        await service.refuseReplace(code: "photo_not_in_room", status: 404)
        let removed = Removed()
        let photo = try makePhoto(0)

        let outcome = await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: removed)
            .replacePhoto(accessToken: "t", roomID: "room", photoAssetID: "gone", with: photo)

        XCTAssertEqual(outcome, .failed(.rejectedAtCommit(code: "photo_not_in_room")))
        XCTAssertTrue(removed.urls.isEmpty)
    }

    func test_replacePhoto_transportFailureAtReplace_isTransport() async throws {
        let service = FakeRoomPhotoService()
        await service.failReplaceTransport()
        let photo = try makePhoto(0)

        let outcome = await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
            .replacePhoto(accessToken: "t", roomID: "room", photoAssetID: "b", with: photo)

        XCTAssertEqual(outcome, .failed(.transport))
    }

    func test_replacePhoto_anUnnormalizedPhotograph_isNotAttempted() async throws {
        let service = FakeRoomPhotoService()
        let outcome = await makeUseCase(service: service, uploader: FakeObjectUploader(), removed: Removed())
            .replacePhoto(accessToken: "t", roomID: "room", photoAssetID: "b", with: try makePhoto(0, normalized: false))

        XCTAssertEqual(outcome, .failed(.notNormalized))
        let declarations = await service.declarations
        XCTAssertTrue(declarations.isEmpty)
    }

    func test_replacePhoto_retryOfACommittedUpload_skipsThePUT() async throws {
        let service = FakeRoomPhotoService()
        await service.markCommitted(clientUploadID: "picked_0")
        let uploader = FakeObjectUploader()

        let outcome = await makeUseCase(service: service, uploader: uploader, removed: Removed())
            .replacePhoto(accessToken: "t", roomID: "room", photoAssetID: "b", with: try makePhoto(0))

        let uploads = await uploader.uploads
        XCTAssertTrue(uploads.isEmpty, "committed bytes are immutable; nothing to PUT")
        guard case .replaced(_, let newID) = outcome else { return XCTFail("expected .replaced, got \(outcome)") }
        XCTAssertEqual(newID, "asset_picked_0")
    }

    // MARK: - Idempotent retry

    func test_retryOfACommittedPhoto_skipsThePUT() async throws {
        let service = FakeRoomPhotoService()
        await service.markCommitted(clientUploadID: "picked_0")
        let uploader = FakeObjectUploader()
        let photos = [try makePhoto(0)]

        let outcome = try await makeUseCase(service: service, uploader: uploader, removed: Removed())
            .execute(accessToken: "t", roomID: "room", photos: photos)

        let uploads = await uploader.uploads
        let assigns = await service.assignCalls
        XCTAssertTrue(uploads.isEmpty, "committed bytes are immutable; nothing to PUT")
        XCTAssertEqual(assigns.first?.assetIDs, ["asset_picked_0"])
        XCTAssertFalse(outcome.hasFailures)
    }
}

// MARK: - Fakes

actor FakeRoomPhotoService: RoomPhotoServicing {
    struct AssignCall: Equatable {
        let roomID: String
        let assetIDs: [String]
    }

    private(set) var declarations: [PhotoUploadDeclaration] = []
    private(set) var assignCalls: [AssignCall] = []
    private var committed: Set<String> = []
    private var rejectedDeclarations: [String: PhotoAPIError] = [:]
    private var commitRefusal: PhotoAPIError?
    private var batchFailure = false

    func markCommitted(clientUploadID: String) { committed.insert(clientUploadID) }
    func rejectDeclaration(clientUploadID: String, status: Int, message: String) {
        rejectedDeclarations[clientUploadID] = PhotoAPIError(statusCode: status, message: message, code: nil, assetID: nil)
    }
    func refuseAtCommit(assetID: String, code: String, status: Int) {
        commitRefusal = PhotoAPIError(statusCode: status, message: nil, code: code, assetID: assetID)
    }
    func failAssignBatch() { batchFailure = true }

    func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket {
        declarations.append(declaration)
        if let rejection = rejectedDeclarations[declaration.clientUploadID] { throw rejection }
        let assetID = "asset_\(declaration.clientUploadID)"
        if committed.contains(declaration.clientUploadID) {
            return PhotoUploadTicket(assetID: assetID, isCommitted: true, upload: nil)
        }
        return PhotoUploadTicket(
            assetID: assetID,
            isCommitted: false,
            upload: .init(
                url: URL(string: "https://storage.test/\(declaration.clientUploadID)")!,
                method: "PUT",
                headers: ["Content-Type": declaration.contentType],
                expiresAt: Date().addingTimeInterval(300)
            )
        )
    }

    func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        assignCalls.append(AssignCall(roomID: roomID, assetIDs: assetIDs))
        if batchFailure { throw IdentityAPIClientError.transport }
        if let refusal = commitRefusal, assetIDs.contains(refusal.assetID ?? "") {
            commitRefusal = nil
            throw refusal
        }
        return assetIDs.enumerated().map { PhotoSlotAssignment(slotIndex: $0.offset, photoAssetID: $0.element, caption: "") }
    }

    func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] { [] }

    func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        fatalError("not used by upload")
    }

    func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
        throw IdentityAPIClientError.invalidResponse
    }

    struct ReplaceCall: Equatable {
        let roomID: String
        let photoAssetID: String
        let replacementAssetID: String
    }

    private(set) var replaceCalls: [ReplaceCall] = []
    private var replaceRefusal: PhotoAPIError?
    private var replaceTransportFailure = false
    private var roomSlots: [PhotoSlotAssignment] = [
        PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a", caption: ""),
        PhotoSlotAssignment(slotIndex: 1, photoAssetID: "b", caption: "kept caption"),
        PhotoSlotAssignment(slotIndex: 2, photoAssetID: "c", caption: "")
    ]

    func refuseReplace(code: String, status: Int) {
        replaceRefusal = PhotoAPIError(statusCode: status, message: nil, code: code, assetID: nil)
    }
    func failReplaceTransport() { replaceTransportFailure = true }

    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
        replaceCalls.append(ReplaceCall(roomID: roomID, photoAssetID: photoAssetID, replacementAssetID: replacementAssetID))
        if replaceTransportFailure { throw IdentityAPIClientError.transport }
        if let refusal = replaceRefusal { throw refusal }
        roomSlots = RoomPhotoReplacement.replacing(photoAssetID, with: replacementAssetID, in: roomSlots)
        return roomSlots
    }

    func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
        throw IdentityAPIClientError.invalidResponse
    }
}

actor FakeObjectUploader: ObjectUploading {
    struct Upload {
        let file: URL
        let instructions: PhotoUploadTicket.UploadInstructions
    }

    private(set) var uploads: [Upload] = []
    private(set) var peakInFlight = 0
    private var inFlight = 0
    private var failing: Set<String> = []
    private let delayNanoseconds: UInt64

    init(delayNanoseconds: UInt64 = 0) {
        self.delayNanoseconds = delayNanoseconds
    }

    func fail(pickedID: String) { failing.insert(pickedID) }

    func upload(file: URL, using instructions: PhotoUploadTicket.UploadInstructions) async throws {
        inFlight += 1
        peakInFlight = max(peakInFlight, inFlight)
        defer { inFlight -= 1 }
        if delayNanoseconds > 0 {
            try await Task.sleep(nanoseconds: delayNanoseconds)
        }
        if failing.contains(instructions.url.lastPathComponent) {
            throw PhotoAPIError(statusCode: 500, message: "storage refused", code: nil, assetID: nil)
        }
        uploads.append(Upload(file: file, instructions: instructions))
    }
}

final class Removed: @unchecked Sendable {
    private let lock = NSLock()
    private var _urls: [URL] = []
    var urls: [URL] { lock.withLock { _urls } }
    func record(_ url: URL) { lock.withLock { _urls.append(url) } }
}
