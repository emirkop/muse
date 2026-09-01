import Foundation

public enum CaptionRules {
    public static let interimMaximumBytes = 500

    public static func characterCount(_ caption: String) -> Int {
        normalised(caption).count
    }

    public static func byteCount(_ caption: String) -> Int {
        normalised(caption).utf8.count
    }

    public static func normalised(_ caption: String) -> String {
        caption.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    public static func isEmpty(_ caption: String) -> Bool {
        normalised(caption).isEmpty
    }

    public enum Rejection: Equatable, Sendable {
        case tooLong(byteCount: Int, limit: Int)
    }

    public static func rejection(for caption: String) -> Rejection? {
        let bytes = byteCount(caption)
        guard bytes <= interimMaximumBytes else {
            return .tooLong(byteCount: bytes, limit: interimMaximumBytes)
        }
        return nil
    }

    public enum CaptionSaveOutcome: Equatable, Sendable {
        case saved
        case rejected(message: String)
        case failed(message: String)
    }

    public static func message(for rejection: Rejection) -> String {
        switch rejection {
        case .tooLong:
            return "That caption is too long. Try something shorter."
        }
    }
}

public enum RoomPhotoCaptions {
    public static func setting(
        _ caption: String,
        forAssetID assetID: String,
        in slots: [PhotoSlotAssignment]
    ) -> [PhotoSlotAssignment] {
        let normalised = CaptionRules.normalised(caption)
        return slots.map { slot in
            guard slot.photoAssetID == assetID else { return slot }
            return PhotoSlotAssignment(
                slotIndex: slot.slotIndex,
                photoAssetID: slot.photoAssetID,
                caption: normalised
            )
        }
    }

    public static func caption(forAssetID assetID: String, in slots: [PhotoSlotAssignment]) -> String? {
        slots.first { $0.photoAssetID == assetID }?.caption
    }

    public static func hasCaption(assetID: String, in slots: [PhotoSlotAssignment]) -> Bool {
        guard let caption = caption(forAssetID: assetID, in: slots) else { return false }
        return !CaptionRules.isEmpty(caption)
    }
}
