import CoreGraphics
import Foundation
import ImageIO
import UIKit
import UniformTypeIdentifiers

enum RoomRenderingVerificationFixture {
    static let roomID = "fixture-room"
    static let accessToken = "fixture-token"

    static func storedDimensions(forSlot slot: Int) -> (width: Int, height: Int) {
        switch slot % 3 {
        case 0: return (3072, 2048)
        case 1: return (2048, 3072)
        default: return (2400, 2400)
        }
    }

    static func makeContent(
        photoCount: Int,
        downloader: PhotoBytesDownloading? = nil,
        deliveredGeometry: RoomVariantGeometry? = nil,
        deliveredSlotTable: RoomVariantSlotTable? = nil,
        bundleRetention: (any AssetBundleRetaining)? = nil
    ) -> RoomRuntimeContent? {
        guard RoomPhotoSlotLayout.supports(photoCount: photoCount), photoCount > 0 else { return nil }

        let room = Room(
            id: roomID,
            name: "Placement fixture",
            variantID: deliveredSlotTable?.variantID ?? PlaceholderRoomSlotTable.variantID,
            privacy: .private,
            photoSlots: (0..<photoCount).map {
                PhotoSlotAssignment(slotIndex: $0, photoAssetID: assetID(forSlot: $0), caption: seedCaption(forSlot: $0))
            },
            sculptures: [SculptureInstance(slotIndex: 1, catalogID: sculptureCatalogID)]
        )
        let table = deliveredSlotTable ?? PlaceholderRoomSlotTable.build()
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            return nil
        }
        let loader = RoomPhotoTextureLoader(
            photoService: FixturePhotoService(photoCount: photoCount),
            downloader: downloader ?? FixturePhotoDownloader()
        )
        let orderStore = FixtureOrderService(room: room)
        return RoomRuntimeContent(
            roomID: roomID,
            accessToken: accessToken,
            geometry: deliveredGeometry.map { .variantBundle($0) } ?? .verificationFixture,
            viewerRole: .owner,
            room: room,
            slotTable: table,
            placements: placements,
            textures: loader,
            photoService: orderStore,
            roomService: orderStore,
            photoReplacer: FixtureReplacer(store: orderStore),
            sculptureModels: FixtureSculptureModelProvider(),
            catalogService: orderStore,
            bundleRetention: bundleRetention
        )
    }

    static let sculptureCatalogID = "fixture-sculpture-placeholder"

    static func fixtureSculptureCatalog() -> [SculptureCatalogEntry] {
        [
            SculptureCatalogEntry(
                id: sculptureCatalogID,
                displayName: "Fixture placeholder (not a sculpture)",
                assetBundle: AssetBundleRef(id: "fixture-bundle", version: 1)
            ),
            SculptureCatalogEntry(
                id: sculptureCatalogID + "-2",
                displayName: "Fixture placeholder 2 (not a sculpture)",
                assetBundle: AssetBundleRef(id: "fixture-bundle", version: 1)
            )
        ]
    }

    struct FixtureSculptureModelProvider: SculptureModelProviding {
        func modelSource(forCatalogID catalogID: String) async -> SculptureModelSource? {
            catalogID.hasPrefix(sculptureCatalogID) ? .verificationFixture : nil
        }
    }

    static func seedCaption(forSlot slot: Int) -> String {
        switch slot {
        case 0: return "Fixture caption — focal wall"
        case 1: return "Fixture caption — first side wall"
        default: return ""
        }
    }

    static func assetID(forSlot slot: Int) -> String { "fixture-asset-\(slot)" }
    static func url(forSlot slot: Int) -> URL { URL(string: "fixture://photos/\(slot)")! }

    struct FixturePhotoService: RoomPhotoServicing {
        let photoCount: Int

        func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket {
            throw IdentityAPIClientError.invalidResponse
        }

        func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] {
            throw IdentityAPIClientError.invalidResponse
        }

        func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
            throw IdentityAPIClientError.invalidResponse
        }

        func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
            throw IdentityAPIClientError.invalidResponse
        }

        func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
            throw IdentityAPIClientError.invalidResponse
        }

        func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
            throw IdentityAPIClientError.invalidResponse
        }

        func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
            (0..<photoCount).map { slot in
                let dims = storedDimensions(forSlot: slot)
                return PhotoDownloadTicket(
                    photoAssetID: assetID(forSlot: slot),
                    url: url(forSlot: slot),
                    expiresAt: Date().addingTimeInterval(300),
                    pixelWidth: dims.width,
                    pixelHeight: dims.height
                )
            }
        }
    }

    final class FixtureOrderService: RoomPhotoServicing, MuseumServicing, CatalogServicing, @unchecked Sendable {
        private let lock = NSLock()
        private var slots: [PhotoSlotAssignment]
        private var sculptures: [SculptureInstance]
        private let room: Room

        init(room: Room) {
            self.room = room
            self.slots = RoomPhotoOrder.normalised(room.photoSlots)
            self.sculptures = RoomSculptures.sorted(room.sculptures)
        }

        func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
            try lock.withLock {
                let known = Set(slots.map(\.photoAssetID))
                guard orderedAssetIDs.count == slots.count, Set(orderedAssetIDs) == known else {
                    throw PhotoAPIError(statusCode: 409, message: nil, code: "order_mismatch", assetID: nil)
                }
                let byAsset = Dictionary(uniqueKeysWithValues: slots.map { ($0.photoAssetID, $0) })
                slots = orderedAssetIDs.enumerated().map { position, assetID in
                    PhotoSlotAssignment(slotIndex: position, photoAssetID: assetID, caption: byAsset[assetID]?.caption ?? "")
                }
                return slots
            }
        }

        func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
            try lock.withLock {
                guard caption.utf8.count <= CaptionRules.interimMaximumBytes else {
                    throw PhotoAPIError(statusCode: 400, message: nil, code: "caption_too_long", assetID: photoAssetID)
                }
                guard slots.contains(where: { $0.photoAssetID == photoAssetID }) else {
                    throw PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: photoAssetID)
                }
                slots = RoomPhotoCaptions.setting(caption, forAssetID: photoAssetID, in: slots)
                return slots
            }
        }

        func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
            try lock.withLock {
                guard !replacementAssetID.isEmpty, replacementAssetID != photoAssetID else {
                    throw PhotoAPIError(statusCode: 400, message: nil, code: "invalid_replacement", assetID: nil)
                }
                guard slots.contains(where: { $0.photoAssetID == photoAssetID }) else {
                    if slots.contains(where: { $0.photoAssetID == replacementAssetID }) { return slots }
                    throw PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: nil)
                }
                guard !slots.contains(where: { $0.photoAssetID == replacementAssetID }) else {
                    throw PhotoAPIError(statusCode: 409, message: nil, code: "asset_already_assigned", assetID: replacementAssetID)
                }
                slots = RoomPhotoReplacement.replacing(photoAssetID, with: replacementAssetID, in: slots)
                return slots
            }
        }

        func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
            try lock.withLock {
                guard slots.contains(where: { $0.photoAssetID == photoAssetID }) else {
                    throw PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: photoAssetID)
                }
                slots = RoomPhotoDeletion.removing(photoAssetID, from: slots)
                return slots
            }
        }

        func fetchRoom(accessToken: String, roomID: String) async throws -> Room {
            lock.withLock { room.replacingPhotoSlots(slots) }
        }

        // MARK: — sculptures, in memory

        func addSculpture(accessToken: String, roomID: String, catalogID: String) async throws -> [SculptureInstance] {
            try lock.withLock {
                guard fixtureSculptureCatalog().contains(where: { $0.id == catalogID }) else {
                    throw PhotoAPIError(statusCode: 400, message: nil, code: "unknown_sculpture", assetID: nil)
                }
                guard let next = RoomSculptures.adding(catalogID, to: sculptures) else {
                    throw PhotoAPIError(statusCode: 409, message: nil, code: "sculpture_capacity_reached", assetID: nil)
                }
                sculptures = next
                return sculptures
            }
        }

        func removeSculpture(accessToken: String, roomID: String, slotIndex: Int) async throws -> [SculptureInstance] {
            try lock.withLock {
                guard RoomSculptures.isOccupied(slotIndex: slotIndex, in: sculptures) else {
                    throw PhotoAPIError(statusCode: 404, message: nil, code: "sculpture_not_in_room", assetID: nil)
                }
                sculptures = RoomSculptures.removing(slotIndex: slotIndex, from: sculptures)
                return sculptures
            }
        }

        func fetchSculptures(accessToken: String) async throws -> [SculptureCatalogEntry] {
            fixtureSculptureCatalog()
        }

        func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket { throw IdentityAPIClientError.invalidResponse }
        func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] { throw IdentityAPIClientError.invalidResponse }
        func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] { [] }
        func createMuseum(accessToken: String, styleID: String) async throws -> Museum { throw IdentityAPIClientError.invalidResponse }
        func fetchMuseum(accessToken: String) async throws -> Museum { throw IdentityAPIClientError.invalidResponse }
        func changeStyle(accessToken: String, styleID: String) async throws -> Museum { throw IdentityAPIClientError.invalidResponse }
        func changePrivacy(accessToken: String, privacy: MusePrivacy) async throws -> Museum { throw IdentityAPIClientError.invalidResponse }
        func createRoom(accessToken: String, name: String, variantID: String) async throws -> Room { throw IdentityAPIClientError.invalidResponse }
        func listRooms(accessToken: String) async throws -> [Room] { [] }
        func updateRoom(accessToken: String, roomID: String, patch: RoomPatch) async throws -> Room { throw IdentityAPIClientError.invalidResponse }
        func assignRoomMusic(accessToken: String, roomID: String, musicTrackID: String) async throws -> Room { throw IdentityAPIClientError.invalidResponse }
        func removeRoomMusic(accessToken: String, roomID: String) async throws -> Room { throw IdentityAPIClientError.invalidResponse }
        func fetchStyles(accessToken: String) async throws -> [MuseumStyle] { [] }
        func fetchVariants(accessToken: String, styleID: String) async throws -> [RoomVariant] { [] }
    }

    struct FixtureReplacer: RoomPhotoReplacing {
        let store: FixtureOrderService

        func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, with photo: PickedPhoto) async -> PhotoReplacementOutcome {
            guard photo.normalizedFile != nil else { return .failed(.notNormalized) }
            let replacementAssetID = "fixture-replacement-\(photo.id)"
            do {
                let slots = try await store.replacePhoto(
                    accessToken: accessToken, roomID: roomID,
                    photoAssetID: photoAssetID, replacementAssetID: replacementAssetID
                )
                return .replaced(photoSlots: slots, replacementAssetID: replacementAssetID)
            } catch let error as PhotoAPIError {
                return .failed(.rejectedAtCommit(code: error.code))
            } catch {
                return .failed(.transport)
            }
        }
    }

    final class FixturePhotoDownloader: PhotoBytesDownloading, @unchecked Sendable {
        private let cache = NSCache<NSURL, NSData>()

        init() {
            cache.countLimit = 32
        }

        func download(_ url: URL) async throws -> Data {
            if let cached = cache.object(forKey: url as NSURL) { return cached as Data }
            guard url.scheme == "fixture", let slot = Int(url.lastPathComponent) else {
                throw PhotoDownloadError.transport
            }
            let dims = storedDimensions(forSlot: slot)
            let data = await Task.detached(priority: .utility) {
                Self.renderJPEG(slot: slot, width: dims.width, height: dims.height)
            }.value
            cache.setObject(data as NSData, forKey: url as NSURL)
            return data
        }

        static func renderJPEG(slot: Int, width: Int, height: Int) -> Data {
            let size = CGSize(width: width, height: height)
            let format = UIGraphicsImageRendererFormat()
            format.scale = 1
            format.opaque = true
            let renderer = UIGraphicsImageRenderer(size: size, format: format)
            let image = renderer.image { context in
                let hue = CGFloat(slot % 28) / 28
                UIColor(hue: hue, saturation: 0.45, brightness: 0.85, alpha: 1).setFill()
                context.fill(CGRect(origin: .zero, size: size))

                let text = "\(slot)" as NSString
                let attributes: [NSAttributedString.Key: Any] = [
                    .font: UIFont.systemFont(ofSize: CGFloat(min(width, height)) * 0.5, weight: .black),
                    .foregroundColor: UIColor(white: 0.12, alpha: 1)
                ]
                let textSize = text.size(withAttributes: attributes)
                text.draw(
                    at: CGPoint(x: (size.width - textSize.width) / 2, y: (size.height - textSize.height) / 2),
                    withAttributes: attributes
                )
            }
            return image.jpegData(compressionQuality: 0.85) ?? Data()
        }
    }
}
