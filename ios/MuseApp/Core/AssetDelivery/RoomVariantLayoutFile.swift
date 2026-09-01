import Foundation
import simd

public enum RoomVariantLayoutFile {
    public static let supportedFormatVersion = 1

    public enum DecodingFailure: Error, Equatable {
        case unreadable
        case unsupportedFormatVersion(Int)
        case unknownWall(String)
        case missingEntryPoint
        case noPhotoTransforms
    }

    public struct Decoded: Sendable {
        public let table: RoomVariantSlotTable
        public let entry: MuseumCameraSubject
    }

    public static func decode(contentsOf url: URL) throws -> Decoded {
        guard let data = try? Data(contentsOf: url) else {
            throw DecodingFailure.unreadable
        }
        return try decode(data)
    }

    public static func decode(_ data: Data) throws -> Decoded {
        let body: LayoutBody
        do {
            body = try JSONDecoder().decode(LayoutBody.self, from: data)
        } catch {
            throw DecodingFailure.unreadable
        }
        guard body.formatVersion == supportedFormatVersion else {
            throw DecodingFailure.unsupportedFormatVersion(body.formatVersion)
        }
        guard let entry = body.entry else {
            throw DecodingFailure.missingEntryPoint
        }
        guard !body.photoTransforms.isEmpty else {
            throw DecodingFailure.noPhotoTransforms
        }

        var photos: [SlotAnchor: SlotTransform] = [:]
        for entry in body.photoTransforms {
            guard let wall = RoomWall(rawValue: entry.wall) else {
                throw DecodingFailure.unknownWall(entry.wall)
            }
            photos[SlotAnchor(wall: wall, positionOnWall: entry.positionOnWall)] = entry.transform()
        }

        var sculptures: [Int: SlotTransform] = [:]
        for entry in body.sculptureTransforms {
            sculptures[entry.slotIndex] = entry.transform()
        }

        return Decoded(
            table: RoomVariantSlotTable(
                variantID: body.variantID,
                photoTransforms: photos,
                sculptureTransforms: sculptures
            ),
            entry: MuseumCameraSubject(position: entry.simdPosition, yaw: entry.yaw)
        )
    }
}

// MARK: - File shapes

private struct LayoutBody: Decodable {
    let formatVersion: Int
    let variantID: String
    let entry: EntryBody?
    let photoTransforms: [PhotoTransformBody]
    let sculptureTransforms: [SculptureTransformBody]

    enum CodingKeys: String, CodingKey {
        case formatVersion = "format_version"
        case variantID = "variant_id"
        case entry
        case photoTransforms = "photo_transforms"
        case sculptureTransforms = "sculpture_transforms"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        formatVersion = try container.decode(Int.self, forKey: .formatVersion)
        variantID = try container.decode(String.self, forKey: .variantID)
        entry = try container.decodeIfPresent(EntryBody.self, forKey: .entry)
        photoTransforms = try container.decodeIfPresent([PhotoTransformBody].self, forKey: .photoTransforms) ?? []
        sculptureTransforms = try container.decodeIfPresent([SculptureTransformBody].self, forKey: .sculptureTransforms) ?? []
    }
}

private struct EntryBody: Decodable {
    let position: [Float]
    let yaw: Float

    var simdPosition: SIMD3<Float> {
        SIMD3<Float>(position.count == 3 ? position[0] : 0,
                     position.count == 3 ? position[1] : 0,
                     position.count == 3 ? position[2] : 0)
    }
}

private protocol TransformBody {
    var position: [Float] { get }
    var rotation: [Float] { get }
    var scale: [Float] { get }
}

extension TransformBody {
    func transform() -> SlotTransform {
        SlotTransform(
            position: vector(position, default: 0),
            rotation: quaternion(rotation),
            scale: vector(scale, default: 1)
        )
    }

    private func vector(_ values: [Float], default fallback: Float) -> SIMD3<Float> {
        guard values.count == 3 else { return SIMD3<Float>(repeating: fallback) }
        return SIMD3<Float>(values[0], values[1], values[2])
    }

    private func quaternion(_ values: [Float]) -> simd_quatf {
        guard values.count == 4 else { return simd_quatf(ix: 0, iy: 0, iz: 0, r: 1) }
        return simd_quatf(ix: values[0], iy: values[1], iz: values[2], r: values[3])
    }
}

private struct PhotoTransformBody: Decodable, TransformBody {
    let wall: String
    let positionOnWall: Int
    let position: [Float]
    let rotation: [Float]
    let scale: [Float]

    enum CodingKeys: String, CodingKey {
        case wall
        case positionOnWall = "position_on_wall"
        case position
        case rotation
        case scale
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        wall = try container.decode(String.self, forKey: .wall)
        positionOnWall = try container.decodeIfPresent(Int.self, forKey: .positionOnWall) ?? 0
        position = try container.decodeIfPresent([Float].self, forKey: .position) ?? []
        rotation = try container.decodeIfPresent([Float].self, forKey: .rotation) ?? []
        scale = try container.decodeIfPresent([Float].self, forKey: .scale) ?? []
    }
}

private struct SculptureTransformBody: Decodable, TransformBody {
    let slotIndex: Int
    let position: [Float]
    let rotation: [Float]
    let scale: [Float]

    enum CodingKeys: String, CodingKey {
        case slotIndex = "slot_index"
        case position
        case rotation
        case scale
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        slotIndex = try container.decode(Int.self, forKey: .slotIndex)
        position = try container.decodeIfPresent([Float].self, forKey: .position) ?? []
        rotation = try container.decodeIfPresent([Float].self, forKey: .rotation) ?? []
        scale = try container.decodeIfPresent([Float].self, forKey: .scale) ?? []
    }
}
