import Foundation
import simd

enum CollectionItemPlacementFixture {

    static let designID = "dev-fixture:collection-item-placement"

    static let cumulativeCapacities = [4, 10, 18]

    static func tierTable() -> CollectionTierTable {
        var tiers: [CollectionTierTable.Tier] = []
        var slotIndex = 0
        var previousCapacity = 0

        for (offset, capacity) in cumulativeCapacities.enumerated() {
            let added = capacity - previousCapacity
            var slots: [CollectionItemSlot] = []
            for column in 0..<added {
                let spacing: Float = 0.55
                let x = (Float(column) - Float(added - 1) / 2) * spacing
                slots.append(CollectionItemSlot(
                    slotIndex: slotIndex,
                    transform: SlotTransform(
                        position: SIMD3<Float>(x, 0.9 + Float(offset) * 0.45, -1.4 - Float(offset) * 0.9),
                        rotation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0)),
                        scale: SIMD3<Float>(0.42, 0.42, 0.42)
                    )
                ))
                slotIndex += 1
            }
            tiers.append(CollectionTierTable.Tier(
                ordinal: CollectionTier.base.ordinal + offset,
                cumulativeCapacity: capacity,
                itemTransforms: slots,
                additionalGeometry: nil
            ))
            previousCapacity = capacity
        }
        return CollectionTierTable(
            designID: designID,
            tiers: tiers,
            entry: SlotTransform(
                position: SIMD3<Float>(0, 0, 1.6),
                rotation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0))
            )
        )
    }

    static func room(itemCount: Int) -> CollectionRoom {
        let table = tierTable()
        let tier: CollectionTier
        switch table.capacities.requiredTier(forItemCount: itemCount) {
        case .success(let required): tier = required
        case .failure: tier = table.highestTier ?? .base
        }
        return CollectionRoom(
            id: "dev-fixture-collection-room",
            name: "Placement fixture (not a Collection Room)",
            categoryID: "category_watches",
            designID: designID,
            currentTier: tier,
            items: (0..<itemCount).map {
                CollectionItem(
                    id: "dev-fixture-item-\($0)",
                    slotIndex: $0,
                    catalogModelID: "dev-fixture:model-chrono-one"
                )
            }
        )
    }

    static let offeredItemCounts = [1, 3, 4, 5, 10, 11, 18]
}

actor FixtureCollectionItemStore: CollectionItemPlacing, CollectionServicing {
    private var room: CollectionRoom
    private var nextFailure: Error?

    func failNextWrite() {
        nextFailure = CollectionAPIError(statusCode: 500, code: nil, message: "fixture failure")
    }

    init(room: CollectionRoom) {
        self.room = room
    }

    // MARK: - CollectionItemPlacing

    func addItem(
        accessToken: String,
        collectionRoomID: String,
        catalogModelID: String
    ) async throws -> CollectionRoom {
        if let failure = takeFailure() { throw failure }

        let occupied = Set(room.items.map(\.slotIndex))
        var slot = 0
        while occupied.contains(slot) { slot += 1 }
        let item = CollectionItem(
            id: "dev-fixture-item-added-\(UUID().uuidString.prefix(8))",
            slotIndex: slot,
            catalogModelID: catalogModelID
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
        if let failure = takeFailure() { throw failure }

        guard let moving = room.items.first(where: { $0.id == collectionItemID }) else {
            throw CollectionAPIError(statusCode: 404, code: "item_not_in_room", message: "not found")
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

    // MARK: - CollectionServicing (only the read the coordinator uses)

    func fetchCollectionRoom(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom {
        return room
    }

    func createCollectionRoom(
        accessToken: String, name: String, categoryID: String?, designID: String?
    ) async throws -> CollectionRoom {
        throw CollectionAPIError(statusCode: 501, code: nil, message: "fixture")
    }

    func listCollectionRooms(accessToken: String) async throws -> [CollectionRoom] {
        return [room]
    }

    func updateCollectionRoom(
        accessToken: String, collectionRoomID: String, patch: CollectionRoomPatch
    ) async throws -> CollectionRoom {
        throw CollectionAPIError(statusCode: 501, code: nil, message: "fixture")
    }

    func deleteCollectionRoom(accessToken: String, collectionRoomID: String) async throws {
        throw CollectionAPIError(statusCode: 501, code: nil, message: "fixture")
    }

    func setFixtureTier(_ tier: CollectionTier) -> CollectionRoom {
        guard tier > room.currentTier else { return room }
        room = CollectionRoom(
            id: room.id,
            name: room.name,
            categoryID: room.categoryID,
            designID: room.designID,
            currentTier: tier,
            items: room.items
        )
        return room
    }

    private func takeFailure() -> Error? {
        guard let failure = nextFailure else { return nil }
        nextFailure = nil
        return failure
    }
}
