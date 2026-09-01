import Foundation
import simd

public enum CollectionDesignLayoutFile {
    public static let supportedFormatVersion = 1

    public enum DecodingFailure: Error, Equatable {
        case unreadable
        case unsupportedFormatVersion(Int)
        case missingEntryPoint
        case incoherentTierTable(CollectionTierTable.Rejection)
    }

    public static func decode(contentsOf url: URL) throws -> CollectionTierTable {
        guard let data = try? Data(contentsOf: url) else {
            throw DecodingFailure.unreadable
        }
        return try decode(data)
    }

    public static func decode(_ data: Data) throws -> CollectionTierTable {
        let file: File
        do {
            file = try JSONDecoder().decode(File.self, from: data)
        } catch {
            throw DecodingFailure.unreadable
        }
        guard file.formatVersion == supportedFormatVersion else {
            throw DecodingFailure.unsupportedFormatVersion(file.formatVersion)
        }
        guard let entry = file.entry?.slotTransform else {
            throw DecodingFailure.missingEntryPoint
        }

        let table = CollectionTierTable(
            designID: file.designID,
            tiers: file.tiers.map { tier in
                CollectionTierTable.Tier(
                    ordinal: tier.tier,
                    cumulativeCapacity: tier.cumulativeCapacity,
                    itemTransforms: tier.itemTransforms.map {
                        CollectionItemSlot(slotIndex: $0.slotIndex, transform: $0.slotTransform)
                    },
                    additionalGeometry: tier.geometryBundle?.reference
                )
            },
            entry: entry
        )

        if let rejection = table.rejection {
            throw DecodingFailure.incoherentTierTable(rejection)
        }
        return table
    }

    // MARK: - File shape

    private struct File: Decodable {
        let formatVersion: Int
        let designID: String
        let entry: Placement?
        let tiers: [TierEntry]

        enum CodingKeys: String, CodingKey {
            case formatVersion = "format_version"
            case designID = "design_id"
            case entry, tiers
        }
    }

    private struct TierEntry: Decodable {
        let tier: Int
        let cumulativeCapacity: Int
        let geometryBundle: BundleReference?
        let itemTransforms: [SlotPlacement]

        enum CodingKeys: String, CodingKey {
            case tier
            case cumulativeCapacity = "cumulative_capacity"
            case geometryBundle = "geometry_bundle"
            case itemTransforms = "item_transforms"
        }
    }

    private struct BundleReference: Decodable {
        let id: String
        let version: Int
        var reference: AssetBundleRef { AssetBundleRef(id: id, version: version) }
    }

    private struct Placement: Decodable {
        let position: [Float]
        let yaw: Float?

        var slotTransform: SlotTransform? {
            guard position.count == 3 else { return nil }
            return SlotTransform(
                position: SIMD3<Float>(position[0], position[1], position[2]),
                rotation: simd_quatf(angle: yaw ?? 0, axis: SIMD3<Float>(0, 1, 0))
            )
        }
    }

    private struct SlotPlacement: Decodable {
        let slotIndex: Int
        let position: [Float]
        let yaw: Float?
        let scale: [Float]?

        enum CodingKeys: String, CodingKey {
            case slotIndex = "slot_index"
            case position, yaw, scale
        }

        var slotTransform: SlotTransform {
            let point = position.count == 3
                ? SIMD3<Float>(position[0], position[1], position[2])
                : SIMD3<Float>(repeating: 0)
            let envelope = (scale?.count == 3)
                ? SIMD3<Float>(scale![0], scale![1], scale![2])
                : SIMD3<Float>(repeating: 1)
            return SlotTransform(
                position: point,
                rotation: simd_quatf(angle: yaw ?? 0, axis: SIMD3<Float>(0, 1, 0)),
                scale: envelope
            )
        }
    }
}
