import XCTest
import simd
@testable import MuseApp

// MARK: - Fixtures

func makeTierTable(
    capacities: [Int],
    designID: String = "dev-fixture:collection-design",
    geometryFromTier: Int = 2
) -> CollectionTierTable {
    var tiers: [CollectionTierTable.Tier] = []
    var previous = 0
    var slotIndex = 0
    for (offset, cumulative) in capacities.enumerated() {
        let ordinal = offset + 1
        var slots: [CollectionItemSlot] = []
        for _ in 0..<(cumulative - previous) {
            slots.append(CollectionItemSlot(
                slotIndex: slotIndex,
                transform: SlotTransform(position: SIMD3<Float>(Float(slotIndex), 1, 0))
            ))
            slotIndex += 1
        }
        tiers.append(CollectionTierTable.Tier(
            ordinal: ordinal,
            cumulativeCapacity: cumulative,
            itemTransforms: slots,
            additionalGeometry: ordinal >= geometryFromTier
                ? AssetBundleRef(id: "bundle_tier\(ordinal)", version: 1)
                : nil
        ))
        previous = cumulative
    }
    return CollectionTierTable(
        designID: designID,
        tiers: tiers,
        entry: SlotTransform(position: SIMD3<Float>(0, 0, 3))
    )
}

let fixtureTierTable = makeTierTable(capacities: [4, 10, 18])

final class FakeTierRatchet: CollectionTierRatcheting, @unchecked Sendable {
    private(set) var requests: [Int] = []
    var storedTier: CollectionTier = .base
    var authoredTiers = 3
    var failure: Error?

    func ratchetTier(
        accessToken: String,
        collectionRoomID: String,
        tier: CollectionTier
    ) async throws -> CollectionRoom {
        requests.append(tier.ordinal)
        if let failure { throw failure }
        guard tier.ordinal >= 1, tier.ordinal <= authoredTiers else {
            throw CollectionAPIError(statusCode: 400, code: "tier_not_authored", message: nil)
        }
        storedTier = CollectionTier(max(storedTier.ordinal, tier.ordinal))
        return CollectionRoom(
            id: collectionRoomID, name: "Watches",
            categoryID: "category_watches", designID: "dev-fixture:collection-design",
            currentTier: storedTier
        )
    }
}

final class FakeTierGeometry: CollectionTierGeometryInstalling, @unchecked Sendable {
    private(set) var installed: [AssetBundleRef] = []
    var failing: Set<String> = []

    func installTierGeometry(accessToken: String, bundle: AssetBundleRef) async -> Bool {
        installed.append(bundle)
        return !failing.contains(bundle.id)
    }
}

private func room(tier: CollectionTier, itemCount: Int = 0) -> CollectionRoom {
    CollectionRoom(
        id: "collection-1", name: "Watches",
        categoryID: "category_watches", designID: "dev-fixture:collection-design",
        currentTier: tier,
        items: (0..<itemCount).map {
            CollectionItem(id: "i\($0)", slotIndex: $0, catalogModelID: "dev-synthetic:item-\($0)")
        }
    )
}

// MARK: - The table itself

final class CollectionTierTableTests: XCTestCase {

    func test_theFixtureTableIsCoherent() {
        XCTAssertNil(fixtureTierTable.rejection)
        XCTAssertEqual(fixtureTierTable.capacities.cumulative, [4, 10, 18])
        XCTAssertEqual(fixtureTierTable.highestTier, CollectionTier(3))
        XCTAssertEqual(fixtureTierTable.tiers.map(\.itemTransforms.count), [4, 6, 8])
    }

    func test_incoherentTablesAreRejected() {
        let slot = { (index: Int) in
            CollectionItemSlot(slotIndex: index, transform: SlotTransform(position: .zero))
        }
        func table(_ tiers: [CollectionTierTable.Tier]) -> CollectionTierTable {
            CollectionTierTable(designID: "d", tiers: tiers, entry: SlotTransform(position: .zero))
        }

        XCTAssertEqual(table([]).rejection, .noTiers)

        XCTAssertEqual(
            table([.init(ordinal: 2, cumulativeCapacity: 4, itemTransforms: (0..<4).map(slot))]).rejection,
            .tiersNotSequentialFromOne
        )
        XCTAssertEqual(
            table([
                .init(ordinal: 1, cumulativeCapacity: 4, itemTransforms: (0..<4).map(slot)),
                .init(ordinal: 2, cumulativeCapacity: 4, itemTransforms: [])
            ]).rejection,
            .capacitiesNotStrictlyIncreasing
        )
        XCTAssertEqual(
            table([.init(ordinal: 1, cumulativeCapacity: 4, itemTransforms: (0..<3).map(slot))]).rejection,
            .slotCountDoesNotMatchAddedCapacity(tier: 1)
        )
        XCTAssertEqual(
            table([.init(ordinal: 1, cumulativeCapacity: 2, itemTransforms: [slot(0), slot(5)])]).rejection,
            .slotIndicesNotContiguous
        )
    }

    func test_slotsBecomeAvailableOnlyWhenTheirTierIsReached() {
        XCTAssertEqual(fixtureTierTable.availableSlots(atTier: CollectionTier(1)).count, 4)
        XCTAssertEqual(fixtureTierTable.availableSlots(atTier: CollectionTier(2)).count, 10)
        XCTAssertEqual(fixtureTierTable.availableSlots(atTier: CollectionTier(3)).count, 18)

        for ordinal in 1...3 {
            let slots = fixtureTierTable.availableSlots(atTier: CollectionTier(ordinal))
            XCTAssertEqual(slots.map(\.slotIndex), Array(0..<slots.count))
        }

        XCTAssertNil(fixtureTierTable.slot(forSlotIndex: 12, atTier: CollectionTier(1)))
        XCTAssertNil(fixtureTierTable.slot(forSlotIndex: 12, atTier: CollectionTier(2)))
        XCTAssertEqual(fixtureTierTable.slot(forSlotIndex: 12, atTier: CollectionTier(3))?.slotIndex, 12)

        XCTAssertNil(fixtureTierTable.slot(forSlotIndex: 18, atTier: CollectionTier(3)))
        XCTAssertNil(fixtureTierTable.slot(forSlotIndex: -1, atTier: CollectionTier(3)))
    }

    func test_incrementalGeometryIsOnlyWhatNewTiersAdd() {
        XCTAssertEqual(
            fixtureTierTable.additionalGeometry(movingFrom: CollectionTier(1), to: CollectionTier(2)).map(\.id),
            ["bundle_tier2"]
        )
        XCTAssertEqual(
            fixtureTierTable.additionalGeometry(movingFrom: CollectionTier(1), to: CollectionTier(3)).map(\.id),
            ["bundle_tier2", "bundle_tier3"]
        )
        XCTAssertEqual(
            fixtureTierTable.additionalGeometry(movingFrom: CollectionTier(2), to: CollectionTier(3)).map(\.id),
            ["bundle_tier3"]
        )
        XCTAssertTrue(
            fixtureTierTable.additionalGeometry(movingFrom: CollectionTier(3), to: CollectionTier(3)).isEmpty
        )
        XCTAssertNil(fixtureTierTable.tiers[0].additionalGeometry)
    }
}

// MARK: - The expansion engine

@MainActor
final class CollectionTierExpansionTests: XCTestCase {

    private func makeEngine() -> (CollectionTierExpansion, FakeTierRatchet, FakeTierGeometry) {
        let ratchet = FakeTierRatchet()
        let geometry = FakeTierGeometry()
        return (CollectionTierExpansion(ratchet: ratchet, geometry: geometry), ratchet, geometry)
    }

    func test_withinTierOne_doesNotExpand() async {
        let (engine, ratchet, geometry) = makeEngine()

        for count in [0, 1, 4] {
            let result = await engine.expand(
                room: room(tier: .base), toHold: count, table: fixtureTierTable, accessToken: "t"
            )
            guard case .success(let outcome) = result else {
                return XCTFail("expected success for \(count) items, got \(result)")
            }
            XCTAssertEqual(outcome.tier, .base)
            XCTAssertFalse(outcome.expanded)
            XCTAssertTrue(outcome.installedGeometry.isEmpty)
        }
        XCTAssertTrue(ratchet.requests.isEmpty, "no tier request may be made when nothing was crossed")
        XCTAssertTrue(geometry.installed.isEmpty, "no bundle may be fetched when nothing was crossed")
    }

    func test_crossingACapacityExpandsExactlyOneTier() async {
        let (engine, ratchet, geometry) = makeEngine()

        let result = await engine.expand(
            room: room(tier: .base), toHold: 5, table: fixtureTierTable, accessToken: "t"
        )
        guard case .success(let outcome) = result else { return XCTFail("\(result)") }

        XCTAssertEqual(outcome.tier, CollectionTier(2))
        XCTAssertTrue(outcome.expanded)
        XCTAssertEqual(ratchet.requests, [2])
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier2"])
        XCTAssertEqual(outcome.installedGeometry.map(\.id), ["bundle_tier2"])
        XCTAssertTrue(outcome.isFullyRendered)
    }

    func test_jumpingMultipleThresholdsReachesTheCorrectTierInOneRequest() async {
        let (engine, ratchet, geometry) = makeEngine()

        let result = await engine.expand(
            room: room(tier: .base), toHold: 15, table: fixtureTierTable, accessToken: "t"
        )
        guard case .success(let outcome) = result else { return XCTFail("\(result)") }

        XCTAssertEqual(outcome.tier, CollectionTier(3))
        XCTAssertEqual(ratchet.requests, [3], "a multi-tier jump is one request, not one per tier")
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier2", "bundle_tier3"])
    }

    func test_expansionNeverRefetchesTheDesignOrAnAlreadyInstalledTier() async {
        let (engine, _, geometry) = makeEngine()

        _ = await engine.expand(room: room(tier: .base), toHold: 5, table: fixtureTierTable, accessToken: "t")
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier2"])

        _ = await engine.expand(
            room: room(tier: CollectionTier(2)), toHold: 15, table: fixtureTierTable, accessToken: "t"
        )
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier2", "bundle_tier3"])

        XCTAssertFalse(
            geometry.installed.contains(where: { $0.id == "dev_fixture_collection_design" }),
            "a tier expansion must never re-download the Design itself"
        )
    }

    func test_fewerItemsNeverRetractTheTier() async {
        let (engine, ratchet, geometry) = makeEngine()

        let result = await engine.expand(
            room: room(tier: CollectionTier(3)), toHold: 0, table: fixtureTierTable, accessToken: "t"
        )
        guard case .success(let outcome) = result else { return XCTFail("\(result)") }

        XCTAssertEqual(outcome.tier, CollectionTier(3), "the room is intentionally ever-expanding")
        XCTAssertFalse(outcome.expanded)
        XCTAssertTrue(ratchet.requests.isEmpty, "no downward request may be made")
        XCTAssertTrue(geometry.installed.isEmpty)
    }

    func test_beyondTheHighestTier_failsExplicitly() async {
        let (engine, ratchet, geometry) = makeEngine()

        let result = await engine.expand(
            room: room(tier: .base), toHold: 19, table: fixtureTierTable, accessToken: "t"
        )
        guard case .failure(let failure) = result else { return XCTFail("expected failure, got \(result)") }
        XCTAssertEqual(failure, .capacityExhausted(itemCount: 19, highestTier: CollectionTier(3)))

        XCTAssertTrue(ratchet.requests.isEmpty, "nothing may be requested for a count that cannot be held")
        XCTAssertTrue(geometry.installed.isEmpty)
    }

    func test_anIncoherentTableIsRefusedBeforeAnyRequest() async {
        let (engine, ratchet, _) = makeEngine()
        let broken = CollectionTierTable(designID: "d", tiers: [], entry: SlotTransform(position: .zero))

        let result = await engine.expand(room: room(tier: .base), toHold: 3, table: broken, accessToken: "t")
        guard case .failure(let failure) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(failure, .incoherentTable(.noTiers))
        XCTAssertTrue(ratchet.requests.isEmpty)
    }

    func test_aRefusedRatchetChangesNothing() async {
        let (engine, ratchet, geometry) = makeEngine()
        ratchet.failure = CollectionAPIError(statusCode: 400, code: "tier_not_authored", message: nil)

        let result = await engine.expand(
            room: room(tier: .base), toHold: 5, table: fixtureTierTable, accessToken: "t"
        )
        guard case .failure(let failure) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(failure, .ratchetRefused)
        XCTAssertTrue(geometry.installed.isEmpty, "no geometry may be fetched for a tier that was refused")
    }

    func test_aGeometryFailureIsReportedButTheTierStillMoves() async {
        let (engine, _, geometry) = makeEngine()
        geometry.failing = ["bundle_tier3"]

        let result = await engine.expand(
            room: room(tier: .base), toHold: 15, table: fixtureTierTable, accessToken: "t"
        )
        guard case .success(let outcome) = result else { return XCTFail("\(result)") }

        XCTAssertEqual(outcome.tier, CollectionTier(3), "the tier moved despite the render failure")
        XCTAssertTrue(outcome.expanded)
        XCTAssertEqual(outcome.installedGeometry.map(\.id), ["bundle_tier2"])
        XCTAssertEqual(outcome.failedGeometry.map(\.id), ["bundle_tier3"])
        XCTAssertFalse(outcome.isFullyRendered)
    }

    func test_theIncrementalSetFollowsTheServerNotTheLocalRequest() async {
        let (engine, ratchet, geometry) = makeEngine()
        ratchet.storedTier = CollectionTier(3)

        let result = await engine.expand(
            room: room(tier: .base), toHold: 5, table: fixtureTierTable, accessToken: "t"
        )
        guard case .success(let outcome) = result else { return XCTFail("\(result)") }

        XCTAssertEqual(outcome.tier, CollectionTier(3))
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier2", "bundle_tier3"])
        XCTAssertEqual(ratchet.requests, [2])
    }

    func test_aSingleTierDesignNeverExpands() async {
        let (engine, ratchet, _) = makeEngine()
        let single = makeTierTable(capacities: [6])

        for count in [0, 3, 6] {
            let result = await engine.expand(
                room: room(tier: .base), toHold: count, table: single, accessToken: "t"
            )
            guard case .success(let outcome) = result else { return XCTFail("\(result)") }
            XCTAssertEqual(outcome.tier, .base)
            XCTAssertFalse(outcome.expanded)
        }
        XCTAssertTrue(ratchet.requests.isEmpty)

        let result = await engine.expand(
            room: room(tier: .base), toHold: 7, table: single, accessToken: "t"
        )
        guard case .failure(let failure) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(failure, .capacityExhausted(itemCount: 7, highestTier: .base))
    }

    // MARK: - PERFORMANCE GATE #3 (client half)

    func test_performanceGate3_largeSyntheticCollection() async {
        let large = makeTierTable(capacities: (1...200).map { $0 * 25 })
        XCTAssertNil(large.rejection, "the synthetic table must itself be coherent")
        XCTAssertEqual(large.capacities.cumulative.last, 5000)

        var arithmetic = Date()
        for itemCount in stride(from: 0, through: 5000, by: 250) {
            switch large.capacities.requiredTier(forItemCount: itemCount) {
            case .success(let tier):
                let expected = itemCount == 0 ? 1 : Int(ceil(Double(itemCount) / 25.0))
                XCTAssertEqual(tier.ordinal, expected, "\(itemCount) items")
            case .failure(let failure):
                XCTFail("\(itemCount) items: \(failure)")
            }
        }
        let arithmeticDuration = Date().timeIntervalSince(arithmetic)

        let top = large.highestTier!
        arithmetic = Date()
        var resolved = 0
        for slotIndex in 0..<5000 {
            if let slot = large.slot(forSlotIndex: slotIndex, atTier: top) {
                XCTAssertEqual(slot.slotIndex, slotIndex)
                resolved += 1
            }
        }
        let resolutionDuration = Date().timeIntervalSince(arithmetic)
        XCTAssertEqual(resolved, 5000, "every authored slot must resolve at the top tier")

        arithmetic = Date()
        let available = large.availableSlots(atTier: top)
        let unionDuration = Date().timeIntervalSince(arithmetic)
        XCTAssertEqual(available.count, 5000)

        let (engine, ratchet, geometry) = makeEngine()
        ratchet.authoredTiers = 200
        arithmetic = Date()
        let result = await engine.expand(
            room: room(tier: .base), toHold: 5000, table: large, accessToken: "t"
        )
        let expansionDuration = Date().timeIntervalSince(arithmetic)
        guard case .success(let outcome) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(outcome.tier, CollectionTier(200))
        XCTAssertEqual(ratchet.requests, [200], "a 199-tier jump is still ONE request")
        XCTAssertEqual(geometry.installed.count, 199, "and installs only the tiers it newly entered")

        print("""
         (client, synthetic fixture range — NOT production thresholds):
          tier arithmetic, 21 lookups over a 200-tier table: \(arithmeticDuration * 1000) ms
          slot resolution, 5000 items at the top tier:        \(resolutionDuration * 1000) ms
          available-slot union at the top tier (5000 slots):  \(unionDuration * 1000) ms
          one expansion 1 → 200 (199 bundle installs):        \(expansionDuration * 1000) ms
        """)

        XCTAssertLessThan(resolutionDuration, 2.0,
                          "resolving 5000 slots took \(resolutionDuration)s — expected well under a second")
        XCTAssertLessThan(arithmeticDuration, 0.5,
                          "tier arithmetic took \(arithmeticDuration)s for 21 lookups")
    }
}
