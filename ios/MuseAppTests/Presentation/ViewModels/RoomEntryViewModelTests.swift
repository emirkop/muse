import XCTest
@testable import MuseApp

@MainActor
final class RoomEntryViewModelTests: XCTestCase {

    private func room(photoCount: Int, variantID: String = "style_modern_variant_Hall") -> Room {
        Room(id: "room_1", name: "Trabzon", variantID: variantID, privacy: .private,
             photoSlots: (0..<photoCount).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") })
    }

    private func makeViewModel(room: Room, design: any RoomDesignProviding) -> RoomEntryViewModel {
        RoomEntryViewModel(room: room, design: design, textures: NoTextures(), accessToken: "token")
    }

    func test_withTheProductionProvider_reportsDesignUnavailable_neverAFixture() async {
        let viewModel = makeViewModel(room: room(photoCount: 5), design: UnavailableRoomDesignProvider())

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .designUnavailable(variantID: "style_modern_variant_Hall", reason: .notPublished))
        XCTAssertNil(viewModel.content, "a real Room must never be handed placeholder geometry")
    }

    func test_withADesign_resolvesPlacementsThroughThePlacementEngine() async {
        let table = PlaceholderRoomSlotTable.build()
        let room = room(photoCount: 5, variantID: table.variantID)
        let viewModel = makeViewModel(room: room, design: ScriptedDesignProvider.fixture())

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
        let content = viewModel.content!
        XCTAssertEqual(content.roomID, "room_1")
        XCTAssertEqual(content.geometry, .verificationFixture)
        guard case .resolved(let expected) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            return XCTFail("fixture must resolve")
        }
        XCTAssertEqual(content.placements, expected)
        XCTAssertEqual(content.placements.map(\.caption), room.photoSlots.map(\.caption), "captions travel as content metadata")
    }

    func test_variantMismatch_surfacesAsPlacementUnresolvable() async {
        let viewModel = makeViewModel(room: room(photoCount: 2, variantID: "style_modern_variant_Hall"),
                                      design: ScriptedDesignProvider.fixture())
        await viewModel.load()
        XCTAssertEqual(viewModel.state, .placementUnresolvable(.variantMismatch(expected: "style_modern_variant_Hall", received: PlaceholderRoomSlotTable.variantID)))
        XCTAssertNil(viewModel.content)
    }

    func test_emptyRoom_isReady_withNoPlacements() async {
        let table = PlaceholderRoomSlotTable.build()
        let viewModel = makeViewModel(room: room(photoCount: 0, variantID: table.variantID), design: ScriptedDesignProvider.fixture())
        await viewModel.load()
        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertEqual(viewModel.content?.placements.count, 0)
    }

    func test_contentSummary_statesCountAndWallRoles() {
        let design = UnavailableRoomDesignProvider()
        XCTAssertEqual(makeViewModel(room: room(photoCount: 0), design: design).contentSummary, "No photographs yet.")
        XCTAssertEqual(makeViewModel(room: room(photoCount: 1), design: design).contentSummary, "1 photograph · 1 focal")
        XCTAssertEqual(makeViewModel(room: room(photoCount: 4), design: design).contentSummary, "4 photographs · 1 focal · 1 left · 1 right · 1 rear")
        XCTAssertEqual(makeViewModel(room: room(photoCount: 28), design: design).contentSummary, "28 photographs · 1 focal · 13 left · 13 right · 1 rear")
    }

    func test_photoReplacer_travelsIntoTheContent_andGatesTheReplaceAffordance() async {
        let table = PlaceholderRoomSlotTable.build()
        let room = room(photoCount: 2, variantID: table.variantID)

        let with = RoomEntryViewModel(
            room: room, design: ScriptedDesignProvider.fixture(),
            textures: NoTextures(), accessToken: "t",
            photoService: SpyReorderService(room: room), roomService: FakeMuseumService(), photoReplacer: NoReplacer()
        )
        await with.load()
        XCTAssertEqual(with.state, .ready)
        XCTAssertTrue(with.content!.supportsPhotoReplacement)

        let without = RoomEntryViewModel(
            room: room, design: ScriptedDesignProvider.fixture(),
            textures: NoTextures(), accessToken: "t",
            photoService: SpyReorderService(room: room), roomService: FakeMuseumService()
        )
        await without.load()
        XCTAssertTrue(without.content!.supportsOwnerEditing, "captions and reordering need only persistence")
        XCTAssertFalse(without.content!.supportsPhotoReplacement, "replacement additionally needs a pipeline to upload through")
    }

    func test_retry_reloads() async {
        let provider = ScriptedDesignProvider.unavailable(.network)
        let viewModel = makeViewModel(room: room(photoCount: 1, variantID: PlaceholderRoomSlotTable.variantID), design: provider)
        await viewModel.load()
        XCTAssertEqual(viewModel.state, .designUnavailable(variantID: PlaceholderRoomSlotTable.variantID, reason: .network))

        provider.answer(.available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture)))
        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertEqual(provider.callCount, 2)
    }

    func test_bundleRetention_travelsIntoTheContent() async {
        let registry = ActiveBundleRegistry()
        let table = PlaceholderRoomSlotTable.build()
        let viewModel = RoomEntryViewModel(
            room: room(photoCount: 1, variantID: table.variantID), design: ScriptedDesignProvider.fixture(),
            textures: NoTextures(), accessToken: "t", bundleRetention: registry
        )
        await viewModel.load()
        XCTAssertNotNil(viewModel.content?.bundleRetention)
    }
}

struct NoReplacer: RoomPhotoReplacing {
    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, with photo: PickedPhoto) async -> PhotoReplacementOutcome {
        .failed(.transport)
    }
}

struct NoTextures: RoomPhotoTextureProviding {
    func textures(for placements: [ResolvedPhotoPlacement], roomID: String, accessToken: String, maxLongEdge: Int) -> AsyncStream<RoomPhotoTextureEvent> {
        AsyncStream { $0.finish() }
    }
}
