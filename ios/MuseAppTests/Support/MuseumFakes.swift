import Foundation
@testable import MuseApp

final class FakeMuseumService: MuseumServicing, CatalogServicing, MusicCatalogServicing, @unchecked Sendable {
    var fetchResult: Result<Museum, Error> = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
    var createResult: Result<Museum, Error> = .success(Museum(id: "m1", styleID: "style_modern", privacy: .private))
    var stylesResult: Result<[MuseumStyle], Error> = .success([
        MuseumStyle(id: "style_modern", displayName: "Modern", assetBundle: AssetBundleRef(id: "b1", version: 1)),
        MuseumStyle(id: "style_natural", displayName: "Natural", assetBundle: AssetBundleRef(id: "b2", version: 1)),
        MuseumStyle(id: "style_gothic", displayName: "Gothic", assetBundle: AssetBundleRef(id: "b3", version: 1))
    ])

    private(set) var createCallCount = 0
    private(set) var receivedCreateStyleIDs: [String] = []
    private(set) var changeStyleCallCount = 0
    private(set) var receivedChangeStyleIDs: [String] = []
    private(set) var createRoomCallCount = 0
    private(set) var receivedRoomNames: [String] = []
    private(set) var receivedRoomVariantIDs: [String] = []
    private(set) var receivedVariantStyleIDs: [String] = []

    var roomsResult: Result<[Room], Error> = .success([])
    private(set) var fetchRoomCallCount = 0
    var createRoomResult: Result<Room, Error>?
    var updateRoomResult: Result<Room, Error>?
    private(set) var updateRoomCallCount = 0
    private(set) var receivedUpdateRoomIDs: [String] = []
    private(set) var receivedRoomPatches: [RoomPatch] = []
    private(set) var changePrivacyCallCount = 0
    private(set) var receivedMuseumPrivacies: [MusePrivacy] = []
    var changePrivacyResult: Result<Museum, Error>?
    var variantsResult: Result<[RoomVariant], Error> = .success([
        RoomVariant(id: "v1", styleID: "style_modern", displayName: "Hall", assetBundle: AssetBundleRef(id: "bv1", version: 1)),
        RoomVariant(id: "v2", styleID: "style_modern", displayName: "Gallery", assetBundle: AssetBundleRef(id: "bv2", version: 1))
    ])

    func createMuseum(accessToken: String, styleID: String) async throws -> Museum {
        createCallCount += 1
        receivedCreateStyleIDs.append(styleID)
        return try createResult.get()
    }

    func fetchMuseum(accessToken: String) async throws -> Museum {
        try fetchResult.get()
    }

    func changeStyle(accessToken: String, styleID: String) async throws -> Museum {
        changeStyleCallCount += 1
        receivedChangeStyleIDs.append(styleID)
        return try createResult.get()
    }

    func changePrivacy(accessToken: String, privacy: MusePrivacy) async throws -> Museum {
        changePrivacyCallCount += 1
        receivedMuseumPrivacies.append(privacy)
        if let changePrivacyResult {
            return try changePrivacyResult.get()
        }
        let base = try fetchResult.get()
        return Museum(id: base.id, styleID: base.styleID, privacy: privacy)
    }

    func createRoom(accessToken: String, name: String, variantID: String) async throws -> Room {
        createRoomCallCount += 1
        receivedRoomNames.append(name)
        receivedRoomVariantIDs.append(variantID)
        if let createRoomResult {
            return try createRoomResult.get()
        }
        return Room(id: "r\(createRoomCallCount)", name: name, variantID: variantID, privacy: .private)
    }

    func fetchRoom(accessToken: String, roomID: String) async throws -> Room {
        fetchRoomCallCount += 1
        let rooms = try roomsResult.get()
        guard let room = rooms.first(where: { $0.id == roomID }) else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "not found")
        }
        return room
    }

    func listRooms(accessToken: String) async throws -> [Room] {
        try roomsResult.get()
    }

    func updateRoom(accessToken: String, roomID: String, patch: RoomPatch) async throws -> Room {
        updateRoomCallCount += 1
        receivedUpdateRoomIDs.append(roomID)
        receivedRoomPatches.append(patch)
        if let updateRoomResult {
            return try updateRoomResult.get()
        }
        let base = (try? roomsResult.get())?.first(where: { $0.id == roomID })
            ?? Room(id: roomID, name: "Room", variantID: "v1", privacy: .private)
        return Room(
            id: base.id,
            name: patch.name ?? base.name,
            variantID: patch.variantID ?? base.variantID,
            privacy: patch.privacy ?? base.privacy,
            photoSlots: base.photoSlots,
            sculptures: base.sculptures
        )
    }

    // MARK: - Room music

    var musicCatalog: [MusicTrack] = []
    var musicAudioURLs: [String: MusicAudioURL] = [:]
    private(set) var assignedMusic: [(roomID: String, trackID: String)] = []
    private(set) var removedMusic: [String] = []
    private(set) var audioURLRequests: [String] = []

    func assignRoomMusic(accessToken: String, roomID: String, musicTrackID: String) async throws -> Room {
        guard musicCatalog.contains(where: { $0.id == musicTrackID }) else {
            throw IdentityAPIClientError.server(statusCode: 400, message: "unknown music track")
        }
        assignedMusic.append((roomID: roomID, trackID: musicTrackID))
        let base = (try? roomsResult.get())?.first(where: { $0.id == roomID })
            ?? Room(id: roomID, name: "Room", variantID: "v1", privacy: .private)
        return Room(id: base.id, name: base.name, variantID: base.variantID, privacy: base.privacy,
                    musicTrackID: musicTrackID, photoSlots: base.photoSlots, sculptures: base.sculptures)
    }

    func removeRoomMusic(accessToken: String, roomID: String) async throws -> Room {
        removedMusic.append(roomID)
        let base = (try? roomsResult.get())?.first(where: { $0.id == roomID })
            ?? Room(id: roomID, name: "Room", variantID: "v1", privacy: .private)
        return Room(id: base.id, name: base.name, variantID: base.variantID, privacy: base.privacy,
                    musicTrackID: nil, photoSlots: base.photoSlots, sculptures: base.sculptures)
    }

    func fetchMusicTracks(accessToken: String) async throws -> [MusicTrack] {
        musicCatalog
    }

    func musicAudioURL(accessToken: String, trackID: String) async throws -> MusicAudioURL {
        audioURLRequests.append(trackID)
        guard let url = musicAudioURLs[trackID] else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "music track not found")
        }
        return url
    }

    // MARK: - Sculptures

    var sculptureCatalogResult: Result<[SculptureCatalogEntry], Error> = .success([])
    var sculptures: [SculptureInstance] = []
    private(set) var addSculptureCalls: [String] = []
    private(set) var removeSculptureCalls: [Int] = []
    var addSculptureError: Error?
    var removeSculptureError: Error?
    var beforeAddSculpture: (@Sendable () async -> Void)?

    func addSculpture(accessToken: String, roomID: String, catalogID: String) async throws -> [SculptureInstance] {
        addSculptureCalls.append(catalogID)
        if let beforeAddSculpture { await beforeAddSculpture() }
        if let addSculptureError { throw addSculptureError }
        guard let next = RoomSculptures.adding(catalogID, to: sculptures) else {
            throw PhotoAPIError(statusCode: 409, message: nil, code: "sculpture_capacity_reached", assetID: nil)
        }
        sculptures = next
        return sculptures
    }

    func removeSculpture(accessToken: String, roomID: String, slotIndex: Int) async throws -> [SculptureInstance] {
        removeSculptureCalls.append(slotIndex)
        if let removeSculptureError { throw removeSculptureError }
        guard RoomSculptures.isOccupied(slotIndex: slotIndex, in: sculptures) else {
            throw PhotoAPIError(statusCode: 404, message: nil, code: "sculpture_not_in_room", assetID: nil)
        }
        sculptures = RoomSculptures.removing(slotIndex: slotIndex, from: sculptures)
        return sculptures
    }

    func fetchSculptures(accessToken: String) async throws -> [SculptureCatalogEntry] {
        try sculptureCatalogResult.get()
    }

    func fetchStyles(accessToken: String) async throws -> [MuseumStyle] {
        try stylesResult.get()
    }

    func fetchVariants(accessToken: String, styleID: String) async throws -> [RoomVariant] {
        receivedVariantStyleIDs.append(styleID)
        return try variantsResult.get()
    }
}
