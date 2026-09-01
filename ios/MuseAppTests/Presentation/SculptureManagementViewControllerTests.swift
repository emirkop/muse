import XCTest
@testable import MuseApp

@MainActor
final class SculptureManagementViewControllerTests: XCTestCase {

    private func make(
        placed: [SculptureInstance] = [],
        catalog: Result<[SculptureCatalogEntry], Error> = .success([])
    ) -> (SculptureManagementViewController, RoomSculptureEditCoordinator, FakeMuseumService) {
        let service = FakeMuseumService()
        service.sculptures = RoomSculptures.sorted(placed)
        service.sculptureCatalogResult = catalog
        let coordinator = RoomSculptureEditCoordinator(
            roomID: "room_1", sculptures: placed, service: service, accessToken: "tok"
        )
        let controller = SculptureManagementViewController(
            viewModel: SculptureManagementViewModel(catalog: service, accessToken: "tok"),
            coordinator: coordinator, onFinished: {}
        )
        controller.loadViewIfNeeded()
        return (controller, coordinator, service)
    }

    private func entries(_ ids: String...) -> [SculptureCatalogEntry] {
        ids.map { SculptureCatalogEntry(id: $0, displayName: "Name \($0)", assetBundle: AssetBundleRef(id: "b", version: 1)) }
    }

    private func waitFor(_ condition: @MainActor () -> Bool) async {
        for _ in 0..<500 {
            if condition() { return }
            await Task.yield()
        }
    }

    private func settle(_ controller: SculptureManagementViewController) async {
        await waitFor { controller.testIsLoaded }
    }

    // MARK: - The empty catalog is a state, not an error

    func test_anEmptyCatalog_saysSoPlainly_ratherThanShowingAnError() async {
        let (controller, _, _) = make()
        await settle(controller)

        XCTAssertTrue(controller.testEntries.isEmpty)
        XCTAssertTrue(
            controller.testCatalogMessages.contains(SculptureManagementViewController.emptyCatalogMessage),
            "got \(controller.testCatalogMessages)"
        )
        XCTAssertNil(controller.testNotice, "an empty catalog is not a failure")
    }

    func test_aFailedCatalogLoad_isDistinctFromAnEmptyOne() async {
        let (controller, _, _) = make(catalog: .failure(IdentityAPIClientError.transport))
        await settle(controller)

        XCTAssertTrue(controller.testCatalogMessages.contains(SculptureManagementViewController.catalogFailedMessage))
        XCTAssertFalse(controller.testCatalogMessages.contains(SculptureManagementViewController.emptyCatalogMessage))
    }

    // MARK: - Adding

    func test_addingFromTheCatalog_placesTheSculpture() async {
        let (controller, coordinator, service) = make(catalog: .success(entries("s1", "s2")))
        await settle(controller)
        XCTAssertEqual(controller.testEntries.count, 2)

        controller.testAdd(catalogID: "s1")
        await waitFor { controller.testPlacedRowCount == 1 }

        XCTAssertEqual(coordinator.sculptures.map(\.catalogID), ["s1"])
        XCTAssertEqual(coordinator.sculptures.map(\.slotIndex), [0])
        XCTAssertEqual(service.addSculptureCalls, ["s1"])
        XCTAssertEqual(controller.testPlacedRowCount, 1)
        XCTAssertNil(controller.testNotice)
    }

    func test_atCapacity_theAddActionIsDisabled_withExplanatoryCopy() async {
        let placed = (0..<Room.maxSculptures).map { SculptureInstance(slotIndex: $0, catalogID: "s\($0)") }
        let (controller, _, _) = make(placed: placed, catalog: .success(entries("s1")))
        await settle(controller)

        XCTAssertEqual(controller.testAddButtonEnabled(catalogID: "s1"), false)
        XCTAssertTrue(
            controller.testCatalogMessages.contains(RoomSculptureEditCoordinator.fullMessage),
            "got \(controller.testCatalogMessages)"
        )
    }

    func test_aFailedAdd_reportsTheReasonInline_andLeavesTheRoomUnchanged() async {
        let (controller, coordinator, service) = make(catalog: .success(entries("s1")))
        await settle(controller)
        service.addSculptureError = IdentityAPIClientError.transport

        controller.testAdd(catalogID: "s1")
        await waitFor { controller.testNotice != nil }

        XCTAssertEqual(controller.testNotice, RoomSculptureEditCoordinator.addFailedMessage)
        XCTAssertTrue(coordinator.sculptures.isEmpty)
    }

    // MARK: - Removing

    func test_removingFreesTheSlot_andTheOthersStay() async {
        let placed = (0..<Room.maxSculptures).map { SculptureInstance(slotIndex: $0, catalogID: "s\($0)") }
        let (controller, coordinator, service) = make(placed: placed, catalog: .success(entries("s1")))
        await settle(controller)
        XCTAssertEqual(controller.testPlacedRowCount, 3)

        controller.testRemove(slotIndex: 1)
        await waitFor { controller.testPlacedRowCount == 2 }

        XCTAssertEqual(coordinator.sculptures.map(\.slotIndex), [0, 2], "the slot empties and nothing moves")
        XCTAssertEqual(service.removeSculptureCalls, [1])
        XCTAssertEqual(controller.testPlacedRowCount, 2)
        XCTAssertEqual(controller.testAddButtonEnabled(catalogID: "s1"), true)
    }

    func test_placedSculpturesShowTheirCatalogName_whenKnown() async {
        let (controller, _, _) = make(
            placed: [SculptureInstance(slotIndex: 0, catalogID: "s1")],
            catalog: .success(entries("s1"))
        )
        await settle(controller)

        XCTAssertEqual(controller.testPlacedRowCount, 1)
        XCTAssertEqual(controller.testEntries.first?.displayName, "Name s1")
    }

    func test_capacityCopy_isShown() async {
        let (controller, _, _) = make(placed: [SculptureInstance(slotIndex: 0, catalogID: "s")])
        await settle(controller)
        XCTAssertEqual(controller.testPlacedRowCount, 1)
    }
}
