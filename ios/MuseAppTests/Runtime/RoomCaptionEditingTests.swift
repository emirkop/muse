import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomCaptionEditingTests: XCTestCase {

    private let table = PlaceholderRoomSlotTable.build()

    private func room(_ count: Int, captions: [Int: String] = [:]) -> Room {
        Room(
            id: "room_1", name: "Trabzon", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<count).map {
                PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: captions[$0] ?? "")
            }
        )
    }

    private func makeCoordinator(
        photoCount: Int = 4,
        captions: [Int: String] = [:]
    ) -> (RoomContentEditCoordinator, SpyReorderService, FakeMuseumService) {
        let room = room(photoCount, captions: captions)
        let spy = SpyReorderService(room: room)
        let museum = FakeMuseumService()
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            fatalError("fixture must resolve")
        }
        let coordinator = RoomContentEditCoordinator(
            room: room, slotTable: table, placements: placements,
            photoService: spy, roomService: museum, accessToken: "tok"
        )
        return (coordinator, spy, museum)
    }

    private func settle() async {
        for _ in 0..<12 { await Task.yield() }
    }

    // MARK: - Adding, editing, clearing

    func test_setCaption_appliesOptimisticallyThenConfirms() async {
        let (coordinator, spy, _) = makeCoordinator()
        var emitted: [[String]] = []
        coordinator.onPlacementsChanged = { emitted.append($0.map(\.caption)) }

        let outcome = await coordinator.setCaption("Trabzon, 1998", forAssetID: "asset_1")

        XCTAssertEqual(outcome, .saved)
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_1"), "Trabzon, 1998")
        XCTAssertEqual(spy.captionCalls.count, 1)
        XCTAssertEqual(spy.captionCalls[0].assetID, "asset_1")
        XCTAssertEqual(spy.captionCalls[0].caption, "Trabzon, 1998")
        XCTAssertEqual(emitted.first, ["", "Trabzon, 1998", "", ""])
        XCTAssertEqual(coordinator.placements[1].caption, "Trabzon, 1998")
    }

    func test_setCaption_isVisibleBeforeTheServerAnswers() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.isCaptionGated = true

        let task = Task { await coordinator.setCaption("typed", forAssetID: "asset_0") }
        await settle()

        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "typed", "applied locally already")
        spy.releaseCaptions()
        let outcome = await task.value
        XCTAssertEqual(outcome, .saved)
    }

    func test_setCaption_trimsBeforeSending() async {
        let (coordinator, spy, _) = makeCoordinator()
        let outcome = await coordinator.setCaption("   padded   ", forAssetID: "asset_0")

        XCTAssertEqual(outcome, .saved)
        XCTAssertEqual(spy.captionCalls[0].caption, "padded", "the server never sees the owner's stray whitespace")
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "padded")
    }

    func test_clearingACaption_sendsTheEmptyStringAndLeavesNoCaption() async {
        let (coordinator, spy, _) = makeCoordinator(captions: [2: "was here"])

        let outcome = await coordinator.setCaption("", forAssetID: "asset_2")

        XCTAssertEqual(outcome, .saved)
        XCTAssertEqual(spy.captionCalls[0].caption, "")
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_2"), "")
    }

    func test_whitespaceOnly_clearsRatherThanStoringSpaces() async {
        let (coordinator, spy, _) = makeCoordinator(captions: [0: "old"])
        _ = await coordinator.setCaption("    ", forAssetID: "asset_0")
        XCTAssertEqual(spy.captionCalls[0].caption, "")
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "")
    }

    // MARK: - Idempotence and refusals

    func test_submittingTheSameCaption_sendsNothing() async {
        let (coordinator, spy, _) = makeCoordinator(captions: [0: "same"])

        let first = await coordinator.setCaption("same", forAssetID: "asset_0")
        let second = await coordinator.setCaption("  same  ", forAssetID: "asset_0")
        XCTAssertEqual(first, .saved)
        XCTAssertEqual(second, .saved, "trimming makes this the same caption")
        XCTAssertTrue(spy.captionCalls.isEmpty, "an unchanged caption must not hit the server")
    }

    func test_clearingAnAlreadyEmptyCaption_sendsNothing() async {
        let (coordinator, spy, _) = makeCoordinator()
        let outcome = await coordinator.setCaption("", forAssetID: "asset_0")
        XCTAssertEqual(outcome, .saved)
        XCTAssertTrue(spy.captionCalls.isEmpty)
    }

    func test_tooLongCaption_isRefusedLocallyWithoutSending() async {
        let (coordinator, spy, _) = makeCoordinator()
        let long = String(repeating: "a", count: CaptionRules.interimMaximumBytes + 1)

        let outcome = await coordinator.setCaption(long, forAssetID: "asset_0")

        guard case .rejected(let message) = outcome else { return XCTFail("expected a local rejection, got \(outcome)") }
        XCTAssertFalse(message.isEmpty)
        XCTAssertTrue(spy.captionCalls.isEmpty)
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "", "nothing changed locally either")
    }

    func test_captionForAPhotographNotInTheRoom_isRefused() async {
        let (coordinator, spy, _) = makeCoordinator()
        let outcome = await coordinator.setCaption("x", forAssetID: "ghost")

        guard case .rejected = outcome else { return XCTFail("expected a rejection, got \(outcome)") }
        XCTAssertTrue(spy.captionCalls.isEmpty)
    }

    // MARK: - Failure: nothing is lost

    func test_transportFailure_restoresThePreviousCaption_andReportsIt() async {
        let (coordinator, spy, _) = makeCoordinator(captions: [1: "previous"])
        spy.nextCaptionResults = [.failure(IdentityAPIClientError.transport)]
        var emitted: [[String]] = []
        coordinator.onPlacementsChanged = { emitted.append($0.map(\.caption)) }

        let outcome = await coordinator.setCaption("attempted", forAssetID: "asset_1")

        guard case .failed(let message) = outcome else { return XCTFail("expected a failure, got \(outcome)") }
        XCTAssertFalse(message.isEmpty)
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_1"), "previous", "the photo's previous caption state is preserved")
        XCTAssertEqual(emitted.last?[1], "previous", "and the scene was re-rendered to it")
    }

    func test_serverRejection_restoresThePreviousCaption() async {
        let (coordinator, spy, _) = makeCoordinator(captions: [0: "previous"])
        spy.nextCaptionResults = [
            .failure(PhotoAPIError(statusCode: 400, message: "Caption exceeds the maximum length.", code: "caption_too_long", assetID: "asset_0"))
        ]

        let outcome = await coordinator.setCaption("something the server refuses", forAssetID: "asset_0")

        XCTAssertEqual(outcome, .failed(message: "Caption exceeds the maximum length."), "the server's own reason is surfaced")
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "previous")
    }

    func test_photoNotInRoom404_reportsThePhotographIsGone() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.nextCaptionResults = [
            .failure(PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: "asset_0"))
        ]

        let outcome = await coordinator.setCaption("x", forAssetID: "asset_0")

        XCTAssertEqual(outcome, .failed(message: RoomContentEditCoordinator.photographGoneMessage))
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "")
    }

    func test_captionFailure_doesNotTouchTheReorderStatus() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.nextCaptionResults = [.failure(IdentityAPIClientError.transport)]

        _ = await coordinator.setCaption("x", forAssetID: "asset_0")

        XCTAssertEqual(coordinator.status, .idle, "captions do not speak through the reorder status")
        XCTAssertNil(coordinator.statusMessage)
    }

    // MARK: - Convergence on the server

    func test_theServersStoredValueWins() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.nextCaptionResults = [.success([
            PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_0", caption: "server's version"),
            PhotoSlotAssignment(slotIndex: 1, photoAssetID: "asset_1", caption: ""),
            PhotoSlotAssignment(slotIndex: 2, photoAssetID: "asset_2", caption: ""),
            PhotoSlotAssignment(slotIndex: 3, photoAssetID: "asset_3", caption: "")
        ])]

        _ = await coordinator.setCaption("mine", forAssetID: "asset_0")

        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "server's version")
        XCTAssertEqual(coordinator.placements[0].caption, "server's version")
    }

    func test_anOlderCaptionResponse_doesNotClobberANewerEdit() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.isCaptionGated = true

        let first = Task { await coordinator.setCaption("first", forAssetID: "asset_0") }
        await settle()
        let second = Task { await coordinator.setCaption("second", forAssetID: "asset_0") }
        await settle()
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "second")

        spy.releaseCaptions()
        _ = await first.value
        _ = await second.value

        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "second", "the owner's latest text stands")
    }

    // MARK: - Independence from ordering

    func test_aCaptionSavedDuringAReorder_survivesBothConfirmations() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 5)
        spy.isGated = true

        coordinator.swap(from: 0, to: 4)
        await settle()
        let optimisticOrder = RoomPhotoOrder.assetIDs(coordinator.room.photoSlots)

        let outcome = await coordinator.setCaption("written mid-reorder", forAssetID: "asset_0")
        XCTAssertEqual(outcome, .saved)

        spy.releaseAll()
        await settle()

        XCTAssertEqual(coordinator.caption(forAssetID: "asset_0"), "written mid-reorder", "the caption survived the reorder response")
        XCTAssertEqual(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots), optimisticOrder, "and the order survived the caption response")
    }

    func test_aCaptionStillInFlight_isNotRevertedByAReorderConfirmation() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 5)
        spy.isCaptionGated = true

        let caption = Task { await coordinator.setCaption("in flight", forAssetID: "asset_2") }
        await settle()
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_2"), "in flight")

        coordinator.swap(from: 0, to: 1)
        await settle()
        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_2"), "in flight", "the owner's unsaved caption stayed on screen")

        spy.releaseCaptions()
        _ = await caption.value
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_2"), "in flight")
    }

    func test_reorderRollback_doesNotDiscardAnInFlightCaption() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 5)
        spy.isCaptionGated = true
        let caption = Task { await coordinator.setCaption("in flight", forAssetID: "asset_3") }
        await settle()

        spy.nextResults = [.failure(IdentityAPIClientError.transport)]
        coordinator.swap(from: 0, to: 1)
        await settle()

        XCTAssertEqual(coordinator.status, .failedTransport)
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_3"), "in flight")
        spy.releaseCaptions()
        _ = await caption.value
    }

    func test_reordering_sendsNoCaptionRequests() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 6, captions: [0: "a", 1: "b"])
        for index in 0..<5 { coordinator.swap(from: index, to: index + 1) }
        await settle()
        spy.releaseAll()
        await settle()

        XCTAssertTrue(spy.captionCalls.isEmpty, "an ordering change is not a caption change")
    }

    // MARK: - No media work

    func test_captionEditing_neverUploadsOrRequestsDeliveryTickets() async {
        let (coordinator, spy, _) = makeCoordinator()
        _ = await coordinator.setCaption("one", forAssetID: "asset_0")
        _ = await coordinator.setCaption("two", forAssetID: "asset_1")
        _ = await coordinator.setCaption("", forAssetID: "asset_0")
        await settle()

        XCTAssertEqual(spy.uploadInitiations, 0, "no upload may be caused by captioning")
        XCTAssertEqual(spy.assignCalls, 0, "no photo assignment may be caused by captioning")
        XCTAssertEqual(spy.deliveryTicketRequests, 0, "no download may be caused by captioning")
        XCTAssertTrue(spy.orders.isEmpty, "and no reorder either")
    }

    func test_captionEditing_leavesOrderAndSlotIndicesUntouched() async {
        let (coordinator, _, _) = makeCoordinator(photoCount: Room.maxPhotos)
        let orderBefore = RoomPhotoOrder.assetIDs(coordinator.room.photoSlots)
        let transformsBefore = coordinator.placements.map(\.transform)

        _ = await coordinator.setCaption("hello", forAssetID: "asset_13")

        XCTAssertEqual(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots), orderBefore)
        XCTAssertEqual(coordinator.room.photoSlots.map(\.slotIndex), Array(0..<Room.maxPhotos))
        XCTAssertEqual(coordinator.placements.map(\.transform), transformsBefore, "no photograph moved")
    }

    // MARK: - Teardown

    func test_deactivate_stopsCaptionMutationAndLateEmissions() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.isCaptionGated = true
        var emissions = 0
        coordinator.onPlacementsChanged = { _ in emissions += 1 }

        let inFlight = Task { await coordinator.setCaption("typed", forAssetID: "asset_0") }
        await settle()
        let emissionsBefore = emissions

        coordinator.deactivate()
        spy.releaseCaptions()
        _ = await inFlight.value

        XCTAssertEqual(emissions, emissionsBefore, "no emission may reach a torn-down scene")
        let after = await coordinator.setCaption("later", forAssetID: "asset_1")
        guard case .failed = after else { return XCTFail("a deactivated coordinator must refuse, got \(after)") }
        XCTAssertEqual(spy.captionCalls.count, 1, "and must not send anything new")
    }
}

@MainActor
final class RoomPhotoTapInteractionTests: XCTestCase {

    private var host: UIView!
    private var arView: ARView!
    private var layer: RoomPhotoLayer!
    private let table = PlaceholderRoomSlotTable.build()

    override func setUp() {
        host = UIView(frame: CGRect(x: 0, y: 0, width: 320, height: 480))
        arView = ARView(frame: host.bounds, cameraMode: .nonAR, automaticallyConfigureSession: false)
        host.addSubview(arView)
        let controlsSibling = UIView(frame: host.bounds)
        host.addSubview(controlsSibling)

        layer = RoomPhotoLayer()
        let anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
        anchor.addChild(layer.root)

        let room = Room(
            id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<4).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }
        )
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            return XCTFail("must resolve")
        }
        layer.mount(placements)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        host = nil
        arView = nil
        layer = nil
    }

    private func makeInteraction(
        isEditing: @escaping () -> Bool,
        onEdit: @escaping (String) -> Void = { _ in }
    ) -> RoomPhotoTapInteraction {
        RoomPhotoTapInteraction(
            gestureHost: host,
            arView: arView,
            layer: layer,
            requiringFailureOf: nil,
            isEditing: isEditing,
            onPhotoTapped: onEdit
        )
    }

    func test_gesturesAttachToAViewThatActuallyReceivesTheTouch() {
        let reorder = RoomReorderInteraction(
            gestureHost: host, arView: arView, layer: layer,
            isEditing: { true }, onSwap: { _, _ in }
        )
        let caption = makeInteraction(isEditing: { true })

        let touchPoint = CGPoint(x: 160, y: 240)
        let hit = host.hitTest(touchPoint, with: nil)
        XCTAssertNotNil(hit)
        XCTAssertFalse(hit === arView, "the sibling above the ARView is what receives the touch")

        let ours: [UIGestureRecognizer?] = [reorder.recognizer, caption.recognizer]
        for recognizer in ours {
            guard let recognizer, let view = recognizer.view else {
                return XCTFail("both Room interactions must be installed")
            }
            XCTAssertTrue(view === host, "\(type(of: recognizer)) must be on the gesture host")
            XCTAssertTrue(hit?.isDescendant(of: view) == true, "\(type(of: recognizer)) is attached where the touch cannot reach it")
        }
        reorder.detach()
    }

    func test_theTapYieldsToTheReorderLongPress() {
        let reorder = RoomReorderInteraction(
            gestureHost: host, arView: arView, layer: layer,
            isEditing: { true }, onSwap: { _, _ in }
        )
        _ = makeInteraction(isEditing: { true })

        let tap = (host.gestureRecognizers ?? []).compactMap { $0 as? UITapGestureRecognizer }.first
        XCTAssertNotNil(tap)
        XCTAssertNotNil(reorder.recognizer)
        XCTAssertEqual(tap?.numberOfTapsRequired, 1)
        reorder.detach()
    }

    func test_neitherInteractionCancelsTouchesInView() {
        let reorder = RoomReorderInteraction(
            gestureHost: host, arView: arView, layer: layer,
            isEditing: { true }, onSwap: { _, _ in }
        )
        _ = makeInteraction(isEditing: { true })

        XCTAssertFalse(host.gestureRecognizers?.isEmpty ?? true)
        for recognizer in host.gestureRecognizers ?? [] {
            XCTAssertFalse(recognizer.cancelsTouchesInView, "movement must keep receiving its touches")
        }
        reorder.detach()
    }

    func test_tapOutsideEditMode_opensNothing() {
        var opened: [String] = []
        let interaction = makeInteraction(isEditing: { false }, onEdit: { opened.append($0) })
        interaction.testTap(assetID: "a0")
        XCTAssertTrue(opened.isEmpty, "photographs are actionable only in Edit Mode")
    }

    func test_tapInEditMode_reportsThatPhotograph() {
        var opened: [String] = []
        let interaction = makeInteraction(isEditing: { true }, onEdit: { opened.append($0) })
        interaction.testTap(assetID: "a2")
        XCTAssertEqual(opened, ["a2"])
    }

    func test_tapOnAPhotographNotMounted_opensNothing() {
        var opened: [String] = []
        let interaction = makeInteraction(isEditing: { true }, onEdit: { opened.append($0) })
        interaction.testTap(assetID: "not-here")
        XCTAssertTrue(opened.isEmpty)
    }

    func test_detach_removesTheGesture() {
        let interaction = makeInteraction(isEditing: { true })
        XCTAssertEqual((host.gestureRecognizers ?? []).count, 1)
        interaction.detach()
        XCTAssertTrue((host.gestureRecognizers ?? []).isEmpty)
    }
}
