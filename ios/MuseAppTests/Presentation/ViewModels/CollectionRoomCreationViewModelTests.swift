import XCTest
@testable import MuseApp

@MainActor
final class CollectionRoomCreationViewModelTests: XCTestCase {

    private func makeViewModel(
        catalog: FakeCollectionCatalog = FakeCollectionCatalog(),
        collections: FakeCollectionService = FakeCollectionService()
    ) -> CollectionRoomCreationViewModel {
        CollectionRoomCreationViewModel(catalog: catalog, collections: collections, accessToken: "token")
    }

    // MARK: - Loading the picker

    func test_startsLoading_thenShowsWhateverTheServerReturned() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        var observed: [CollectionRoomCreationViewModel.State] = [viewModel.state]
        viewModel.onStateChange = { observed.append($0) }

        await viewModel.loadCategories()

        XCTAssertEqual(observed.first, .loadingCategories)
        guard case .ready(let categories, let selected) = viewModel.state else {
            return XCTFail("expected .ready, got \(viewModel.state)")
        }
        XCTAssertEqual(categories, FakeCollectionCatalog.seeded)
        XCTAssertNil(selected, "no category may be selected for the owner by default")
        XCTAssertEqual(catalog.fetchCount, 1)
    }

    func test_aCategoryTheAppHasNeverHeardOf_isOfferedAndUsable() async {
        let surprise = CollectionCategory(id: "category_vinyl", displayName: "Vinyl Records", sortOrder: 60)
        let catalog = FakeCollectionCatalog(categories: FakeCollectionCatalog.seeded + [surprise])
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(catalog: catalog, collections: collections)

        await viewModel.loadCategories()
        XCTAssertTrue(viewModel.availableCategories.contains(surprise))

        viewModel.selectCategory(id: surprise.id)
        viewModel.updateName("Records")
        let room = await viewModel.createCollectionRoom()

        XCTAssertEqual(room?.categoryID, surprise.id)
        XCTAssertEqual(collections.createCalls.first?.categoryID, surprise.id)
    }

    func test_serverOrderIsPreserved() async {
        let reversed = Array(FakeCollectionCatalog.seeded.reversed())
        let viewModel = makeViewModel(catalog: FakeCollectionCatalog(categories: reversed))

        await viewModel.loadCategories()

        XCTAssertEqual(viewModel.availableCategories.map(\.id), reversed.map(\.id))
    }

    func test_noCategories_isAnHonestStateNotAnError() async {
        let viewModel = makeViewModel(catalog: FakeCollectionCatalog(categories: []))

        await viewModel.loadCategories()

        XCTAssertEqual(viewModel.state, .noCategoriesAvailable)
        XCTAssertFalse(viewModel.canCreate, "there is nothing to create in")
        XCTAssertTrue(CollectionRoomCreationViewModel.noCategoriesMessage.contains("coming soon"))
    }

    func test_failureIsRetryable() async {
        let catalog = FakeCollectionCatalog()
        catalog.error = URLError(.notConnectedToInternet)
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.loadCategories()
        guard case .categoriesFailed = viewModel.state else {
            return XCTFail("expected .categoriesFailed, got \(viewModel.state)")
        }
        XCTAssertFalse(viewModel.canCreate)

        catalog.error = nil
        await viewModel.loadCategories()
        guard case .ready = viewModel.state else {
            return XCTFail("a retry should recover, got \(viewModel.state)")
        }
        XCTAssertEqual(catalog.fetchCount, 2)
    }

    // MARK: - Selection

    func test_onlyACategoryTheServerOfferedCanBeSelected() async {
        let viewModel = makeViewModel()
        await viewModel.loadCategories()

        viewModel.selectCategory(id: "category_watches")
        XCTAssertEqual(viewModel.selectedCategoryID, "category_watches")

        viewModel.selectCategory(id: "category_stamps")
        XCTAssertEqual(viewModel.selectedCategoryID, "category_watches")
    }

    func test_selectionIsIgnoredBeforeCategoriesLoad() {
        let viewModel = makeViewModel()

        viewModel.selectCategory(id: "category_watches")

        XCTAssertNil(viewModel.selectedCategoryID)
    }

    // MARK: - The Create gate

    func test_createRequiresBothANameAndACategory() async {
        let viewModel = makeViewModel()
        await viewModel.loadCategories()

        XCTAssertFalse(viewModel.canCreate, "nothing supplied")

        viewModel.updateName("Watches")
        XCTAssertFalse(viewModel.canCreate, "a name without a category is not enough")

        viewModel.selectCategory(id: "category_coins")
        XCTAssertTrue(viewModel.canCreate)

        viewModel.updateName("   ")
        XCTAssertFalse(viewModel.canCreate, "a whitespace-only name is not a name")
    }

    func test_creatingWithoutACategory_reportsItAndSendsNothing() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.loadCategories()
        viewModel.updateName("Watches")

        let room = await viewModel.createCollectionRoom()

        XCTAssertNil(room)
        XCTAssertTrue(collections.createCalls.isEmpty, "no request may be made")
        XCTAssertNotNil(viewModel.creationErrorMessage)
    }

    func test_creatingWithABadName_reportsInlineAndSendsNothing() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.loadCategories()
        viewModel.selectCategory(id: "category_watches")
        viewModel.updateName(String(repeating: "a", count: CollectionRoomNamingRules.interimMaximumLength + 1))

        let room = await viewModel.createCollectionRoom()

        XCTAssertNil(room)
        XCTAssertTrue(collections.createCalls.isEmpty)
        XCTAssertNotNil(viewModel.nameValidationMessage)
    }

    // MARK: - Creating

    func test_successfulCreation_sendsTheTrimmedNameAndNoDesign() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.loadCategories()
        viewModel.selectCategory(id: "category_hot_wheels")
        viewModel.updateName("  My Cars  ")

        let room = await viewModel.createCollectionRoom()

        XCTAssertEqual(
            collections.createCalls,
            [FakeCollectionService.CreateCall(name: "My Cars", categoryID: "category_hot_wheels", designID: nil)],
            " chooses the Design; passing one here would pre-empt it"
        )
        XCTAssertEqual(room?.name, "My Cars")
        XCTAssertEqual(room?.categoryID, "category_hot_wheels")
        XCTAssertNil(room?.designID)
        XCTAssertEqual(room?.currentTier, .base)
        XCTAssertEqual(room?.itemCount, 0)
        XCTAssertNil(viewModel.creationErrorMessage)
    }

    func test_manyCreationsInARow_areNeverBlocked() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.loadCategories()
        viewModel.selectCategory(id: "category_watches")

        for index in 0..<40 {
            viewModel.updateName("Room \(index)")
            let room = await viewModel.createCollectionRoom()
            XCTAssertNotNil(room, "creation #\(index + 1) was blocked")
        }
        XCTAssertEqual(collections.createCalls.count, 40)
        XCTAssertNil(CollectionRoomCreationViewModel.collectionRoomCountCap, "no cap may be invented")
    }

    func test_unknownCategoryRefusal_reloadsThePicker() async {
        let catalog = FakeCollectionCatalog()
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(catalog: catalog, collections: collections)
        await viewModel.loadCategories()
        viewModel.selectCategory(id: "category_watches")
        viewModel.updateName("Watches")

        collections.createError = CollectionAPIError(
            statusCode: 400, code: "unknown_category", message: "category is not in the catalog"
        )
        let room = await viewModel.createCollectionRoom()

        XCTAssertNil(room)
        XCTAssertNotNil(viewModel.creationErrorMessage)
        XCTAssertEqual(catalog.fetchCount, 2, "a stale category list must be refreshed")
    }

    func test_otherRefusals_doNotReloadThePicker() async {
        let catalog = FakeCollectionCatalog()
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(catalog: catalog, collections: collections)
        await viewModel.loadCategories()
        viewModel.selectCategory(id: "category_watches")
        viewModel.updateName("Watches")

        collections.createError = CollectionAPIError(statusCode: 500, code: nil, message: nil)
        let room = await viewModel.createCollectionRoom()
        XCTAssertNil(room)
        XCTAssertNotNil(viewModel.creationErrorMessage)
        XCTAssertEqual(catalog.fetchCount, 1, "a server fault says nothing about the category list")
    }

    func test_isCreatingIsClearedOnBothOutcomes() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.loadCategories()
        viewModel.selectCategory(id: "category_watches")
        viewModel.updateName("Watches")

        _ = await viewModel.createCollectionRoom()
        XCTAssertFalse(viewModel.isCreating)

        collections.createError = CollectionAPIError(statusCode: 500, code: nil, message: nil)
        _ = await viewModel.createCollectionRoom()
        XCTAssertFalse(viewModel.isCreating, "a failure must not leave the button spinning for ever")
    }
}

@MainActor
final class CollectionRoomListViewModelTests: XCTestCase {

    func test_emptyIsAFirstRunStateNotAnError() async {
        let viewModel = CollectionRoomListViewModel(
            collections: FakeCollectionService(), catalog: FakeCollectionCatalog(), accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .empty)
    }

    func test_rowsShowTheCategoryDisplayNameFromTheCatalog() async {
        let collections = FakeCollectionService()
        collections.rooms = [
            CollectionRoom(id: "c1", name: "My Watches", categoryID: "category_watches"),
            CollectionRoom(id: "c2", name: "Cars", categoryID: "category_hot_wheels")
        ]
        let viewModel = CollectionRoomListViewModel(
            collections: collections, catalog: FakeCollectionCatalog(), accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.rooms.count, 2)
        XCTAssertEqual(viewModel.categoryName(for: collections.rooms[0]), "Watches")
        XCTAssertEqual(viewModel.categoryName(for: collections.rooms[1]), "Hot Wheels")
    }

    func test_aFailedCategoryRead_stillListsTheRooms() async {
        let collections = FakeCollectionService()
        collections.rooms = [CollectionRoom(id: "c1", name: "My Watches", categoryID: "category_watches")]
        let catalog = FakeCollectionCatalog()
        catalog.error = URLError(.timedOut)
        let viewModel = CollectionRoomListViewModel(
            collections: collections, catalog: catalog, accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.rooms.count, 1)
        XCTAssertNil(viewModel.categoryName(for: collections.rooms[0]), "no subtitle rather than a wrong one")
    }

    func test_aRoomWithNoCategoryHasNoSubtitle() async {
        let collections = FakeCollectionService()
        let legacy = CollectionRoom(id: "c1", name: "Legacy", categoryID: nil)
        collections.rooms = [legacy]
        let viewModel = CollectionRoomListViewModel(
            collections: collections, catalog: FakeCollectionCatalog(), accessToken: "token"
        )

        await viewModel.load()

        XCTAssertNil(viewModel.categoryName(for: legacy))
    }

    func test_failureIsRetryable() async {
        let collections = FakeCollectionService()
        collections.listError = URLError(.notConnectedToInternet)
        let viewModel = CollectionRoomListViewModel(
            collections: collections, catalog: FakeCollectionCatalog(), accessToken: "token"
        )

        await viewModel.load()
        guard case .failed = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }

        collections.listError = nil
        collections.rooms = [CollectionRoom(id: "c1", name: "Watches", categoryID: "category_watches")]
        await viewModel.load()
        XCTAssertEqual(viewModel.rooms.count, 1)
    }

    func test_insertShowsANewRoomImmediately_andThereIsNoCap() async {
        let collections = FakeCollectionService()
        let viewModel = CollectionRoomListViewModel(
            collections: collections, catalog: FakeCollectionCatalog(), accessToken: "token"
        )
        await viewModel.load()
        XCTAssertEqual(viewModel.state, .empty)

        for index in 0..<30 {
            viewModel.insert(CollectionRoom(id: "c\(index)", name: "Room \(index)", categoryID: "category_coins"))
        }

        XCTAssertEqual(viewModel.rooms.count, 30)
        XCTAssertNil(CollectionRoomListViewModel.countCap, "no cap may be invented")
    }
}
