import Foundation

public enum RoomPhotoDeletion {
    public static func removing(_ assetID: String, from slots: [PhotoSlotAssignment]) -> [PhotoSlotAssignment] {
        let remaining = RoomPhotoOrder.normalised(slots).filter { $0.photoAssetID != assetID }
        return remaining.enumerated().map { position, slot in
            PhotoSlotAssignment(slotIndex: position, photoAssetID: slot.photoAssetID, caption: slot.caption)
        }
    }
}

public enum PhotoDeletionOutcome: Equatable, Sendable {
    case deleted
    case rejected(message: String)
    case failed(message: String)
}
