import Foundation
@testable import MuseApp

final class FakeCollectionCatalog: CollectionCatalogServicing, @unchecked Sendable {
    static let seeded = [
        CollectionCategory(id: "category_watches", displayName: "Watches", sortOrder: 10),
        CollectionCategory(id: "category_hot_wheels", displayName: "Hot Wheels", sortOrder: 20),
        CollectionCategory(id: "category_coins", displayName: "Coins", sortOrder: 30),
        CollectionCategory(id: "category_license_plates", displayName: "License Plates", sortOrder: 40)
    ]

    static let seededDesigns = [
        CollectionDesign(
            id: "dev-fixture:collection-design",
            displayName: "Development Fixture (not a real design)",
            categoryID: nil,
            isDevelopmentFixture: true,
            assetBundle: AssetBundleRef(id: "dev_fixture_collection_design", version: 1),
            sortOrder: 1000
        )
    ]

    var categories: [CollectionCategory]
    var error: Error?
    private(set) var fetchCount = 0

    var designs: [CollectionDesign]
    var designError: Error?
    private(set) var designFetchCount = 0
    private(set) var requestedCategoryIDs: [String] = []

    init(
        categories: [CollectionCategory] = FakeCollectionCatalog.seeded,
        designs: [CollectionDesign] = FakeCollectionCatalog.seededDesigns
    ) {
        self.categories = categories
        self.designs = designs
    }

    func fetchCollectionCategories(accessToken: String) async throws -> [CollectionCategory] {
        fetchCount += 1
        if let error { throw error }
        return categories
    }

    func fetchCollectionDesigns(
        accessToken: String,
        categoryID: String
    ) async throws -> [CollectionDesign] {
        designFetchCount += 1
        requestedCategoryIDs.append(categoryID)
        if let designError { throw designError }
        return designs.filter { $0.categoryID == nil || $0.categoryID == categoryID }
    }

    // MARK: - Manual Search

    static let seededModels = [
        CollectionCatalogModel(
            id: "dev-fixture:model-chrono-one",
            brandID: "dev-fixture:brand-a",
            brandDisplayName: "Devco (development fixture)",
            categoryID: "category_watches",
            displayName: "Devco Chrono One (development fixture)",
            hasAsset: true,
            assetBundle: AssetBundleRef(id: "dev_fixture_collection_model", version: 1),
            isDevelopmentFixture: true
        ),
        CollectionCatalogModel(
            id: "dev-fixture:model-chrono-two",
            brandID: "dev-fixture:brand-a",
            brandDisplayName: "Devco (development fixture)",
            categoryID: "category_watches",
            displayName: "Devco Chrono Two (development fixture)",
            hasAsset: false,
            isDevelopmentFixture: true
        ),
        CollectionCatalogModel(
            id: "dev-fixture:model-diver",
            brandID: "dev-fixture:brand-b",
            brandDisplayName: "Testmark (development fixture)",
            categoryID: "category_watches",
            displayName: "Testmark Diver (development fixture)",
            isDevelopmentFixture: true
        ),
        CollectionCatalogModel(
            id: "dev-fixture:model-racer",
            brandID: "dev-fixture:brand-a",
            brandDisplayName: "Devco (development fixture)",
            categoryID: "category_hot_wheels",
            displayName: "Devco Racer (development fixture)",
            isDevelopmentFixture: true
        )
    ]

    var models: [CollectionCatalogModel] = FakeCollectionCatalog.seededModels
    var searchError: Error?
    private(set) var searchCalls: [(categoryID: String, query: String, limit: Int, cursor: CollectionModelSearchCursor?)] = []
    var gate: (() async -> Void)?

    func searchCollectionModels(
        accessToken: String,
        categoryID: String,
        query: String,
        limit: Int,
        cursor: CollectionModelSearchCursor?
    ) async throws -> CollectionModelSearchPage {
        searchCalls.append((categoryID, query, limit, cursor))
        if let gate { await gate() }
        if let searchError { throw searchError }

        var matches = models.filter { $0.categoryID == categoryID }
        let terms = query
            .lowercased()
            .split(whereSeparator: { !$0.isLetter && !$0.isNumber })
            .map(String.init)
        if !terms.isEmpty {
            matches = matches.filter { model in
                let haystack = "\(model.displayName) \(model.brandDisplayName)".lowercased()
                return terms.allSatisfy { haystack.contains($0) }
            }
        }
        matches.sort { ($0.displayName, $0.id) < ($1.displayName, $1.id) }

        if let cursor {
            matches = matches.filter { ($0.displayName, $0.id) > (cursor.displayName, cursor.id) }
        }

        let effectiveLimit = limit > 0 ? limit : 25
        var next: CollectionModelSearchCursor?
        if matches.count > effectiveLimit {
            let page = Array(matches.prefix(effectiveLimit))
            next = CollectionModelSearchCursor(displayName: page[page.count - 1].displayName, id: page[page.count - 1].id)
            matches = page
        }
        return CollectionModelSearchPage(models: matches, nextCursor: next)
    }
}

final class FakeCollectionService: CollectionServicing, @unchecked Sendable {
    struct CreateCall: Equatable {
        let name: String
        let categoryID: String?
        let designID: String?
    }

    private(set) var createCalls: [CreateCall] = []
    private(set) var patches: [CollectionRoomPatch] = []
    private(set) var deletedIDs: [String] = []

    var rooms: [CollectionRoom] = []
    var createError: Error?
    var listError: Error?
    var updateError: Error?
    var nextID = 1

    func seed(_ room: CollectionRoom) {
        rooms.append(room)
    }

    func createCollectionRoom(
        accessToken: String,
        name: String,
        categoryID: String?,
        designID: String?
    ) async throws -> CollectionRoom {
        createCalls.append(CreateCall(name: name, categoryID: categoryID, designID: designID))
        if let createError { throw createError }
        let room = CollectionRoom(
            id: "collection-\(nextID)",
            name: name,
            categoryID: categoryID,
            designID: designID,
            currentTier: .base
        )
        nextID += 1
        rooms.append(room)
        return room
    }

    func listCollectionRooms(accessToken: String) async throws -> [CollectionRoom] {
        if let listError { throw listError }
        return rooms
    }

    private(set) var fetchedIDs: [String] = []

    func fetchCollectionRoom(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom {
        fetchedIDs.append(collectionRoomID)
        guard let room = rooms.first(where: { $0.id == collectionRoomID }) else {
            throw CollectionAPIError(statusCode: 404, code: nil, message: "not found")
        }
        return room
    }

    func updateCollectionRoom(
        accessToken: String,
        collectionRoomID: String,
        patch: CollectionRoomPatch
    ) async throws -> CollectionRoom {
        patches.append(patch)
        if let updateError { throw updateError }
        guard let index = rooms.firstIndex(where: { $0.id == collectionRoomID }) else {
            throw CollectionAPIError(statusCode: 404, code: nil, message: "not found")
        }
        let existing = rooms[index]
        let updated = CollectionRoom(
            id: existing.id,
            name: patch.name ?? existing.name,
            categoryID: patch.categoryID ?? existing.categoryID,
            designID: patch.designID ?? existing.designID,
            currentTier: existing.currentTier,
            items: existing.items
        )
        rooms[index] = updated
        return updated
    }

    func deleteCollectionRoom(accessToken: String, collectionRoomID: String) async throws {
        deletedIDs.append(collectionRoomID)
        rooms.removeAll { $0.id == collectionRoomID }
    }
}

// MARK: -: item placement and reordering

final class FakeCollectionItemStore: CollectionItemPlacing, @unchecked Sendable {
    struct AddCall: Equatable {
        let collectionRoomID: String
        let catalogModelID: String
    }
    struct PlaceCall: Equatable {
        let collectionRoomID: String
        let collectionItemID: String
        let slotIndex: Int
    }

    private(set) var addCalls: [AddCall] = []
    private(set) var placeCalls: [PlaceCall] = []

    var room: CollectionRoom
    var addError: Error?
    var placeError: Error?
    var gate: (() async -> Void)?

    private var nextItemNumber = 1000

    init(room: CollectionRoom) {
        self.room = room
    }

    func addItem(
        accessToken: String,
        collectionRoomID: String,
        catalogModelID: String
    ) async throws -> CollectionRoom {
        addCalls.append(AddCall(collectionRoomID: collectionRoomID, catalogModelID: catalogModelID))
        if let gate { await gate() }
        if let addError { throw addError }

        let occupied = Set(room.items.map(\.slotIndex))
        var slot = 0
        while occupied.contains(slot) { slot += 1 }
        nextItemNumber += 1
        let item = CollectionItem(
            id: "server-item-\(nextItemNumber)", slotIndex: slot, catalogModelID: catalogModelID
        )
        room = room.replacingItems((room.items + [item]).sorted { $0.slotIndex < $1.slotIndex })
        return room
    }

    func placeItem(
        accessToken: String,
        collectionRoomID: String,
        collectionItemID: String,
        slotIndex: Int
    ) async throws -> CollectionRoom {
        placeCalls.append(PlaceCall(
            collectionRoomID: collectionRoomID,
            collectionItemID: collectionItemID,
            slotIndex: slotIndex
        ))
        if let gate { await gate() }
        if let placeError { throw placeError }

        guard let moving = room.items.first(where: { $0.id == collectionItemID }) else {
            throw CollectionAPIError(statusCode: 404, code: "item_not_in_room", message: nil)
        }
        if moving.slotIndex == slotIndex { return room }

        var updated = room.items
        if let occupantIndex = updated.firstIndex(where: { $0.slotIndex == slotIndex }) {
            let occupant = updated[occupantIndex]
            updated[occupantIndex] = CollectionItem(
                id: occupant.id, slotIndex: moving.slotIndex, catalogModelID: occupant.catalogModelID
            )
        }
        if let movingIndex = updated.firstIndex(where: { $0.id == collectionItemID }) {
            updated[movingIndex] = CollectionItem(
                id: moving.id, slotIndex: slotIndex, catalogModelID: moving.catalogModelID
            )
        }
        room = room.replacingItems(updated.sorted { $0.slotIndex < $1.slotIndex })
        return room
    }
}

final class FakeCollectionDesignTables: CollectionDesignTableProviding, @unchecked Sendable {
    var table: CollectionTierTable?
    private(set) var requests: [(categoryID: String, designID: String)] = []

    init(table: CollectionTierTable?) {
        self.table = table
    }

    func tierTable(
        accessToken: String,
        categoryID: String,
        designID: String
    ) async -> CollectionTierTable? {
        requests.append((categoryID, designID))
        return table
    }
}
