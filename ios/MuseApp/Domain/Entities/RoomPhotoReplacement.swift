import Foundation

public enum RoomPhotoReplacement {
    public static func replacing(
        _ assetID: String,
        with replacementAssetID: String,
        in slots: [PhotoSlotAssignment]
    ) -> [PhotoSlotAssignment] {
        guard assetID != replacementAssetID else { return slots }
        return slots.map { slot in
            guard slot.photoAssetID == assetID else { return slot }
            return PhotoSlotAssignment(
                slotIndex: slot.slotIndex,
                photoAssetID: replacementAssetID,
                caption: slot.caption
            )
        }
    }
}

public enum PhotoReplacementOutcome: Equatable, Sendable {
    case replaced(photoSlots: [PhotoSlotAssignment], replacementAssetID: String)
    case rejected(message: String)
    case failed(PhotoUploadFailure)
}
