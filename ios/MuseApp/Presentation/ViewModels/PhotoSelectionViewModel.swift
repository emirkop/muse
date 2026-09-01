import Foundation

@MainActor
public final class PhotoSelectionViewModel {
    public enum State: Equatable {
        case ready
        case selecting
        case selected([PickedPhoto])
        case uploading(completed: Int, total: Int)
        case committed(PhotoUploadOutcome)
        case commitFailed
        case roomFull
    }

    public private(set) var state: State {
        didSet {
            guard state != oldValue else { return }
            onStateChange?(state)
        }
    }

    public var onStateChange: ((State) -> Void)?

    public let room: Room

    public private(set) var photoSlots: [PhotoSlotAssignment]

    public private(set) var selectedPhotos: [PickedPhoto] = []

    private let uploader: RoomPhotoUploading
    private let accessToken: String

    public init(room: Room, uploader: RoomPhotoUploading, accessToken: String) {
        self.room = room
        self.photoSlots = room.photoSlots
        self.uploader = uploader
        self.accessToken = accessToken
        self.state = room.remainingPhotoCapacity > 0 ? .ready : .roomFull
    }

    // MARK: - Capacity (the confirmed 28-photo cap)

    public var existingPhotoCount: Int { photoSlots.count }

    public var remainingCapacity: Int { max(0, Room.maxPhotos - existingPhotoCount) }

    public var isRoomFull: Bool { remainingCapacity == 0 }

    public var selectionLimit: Int { remainingCapacity }

    public var projectedPhotoCount: Int { existingPhotoCount + selectedPhotos.count }

    // MARK: - Flow

    public func beginSelection() {
        guard !isRoomFull else {
            state = .roomFull
            return
        }
        state = .selecting
    }

    public func ingest(_ photos: [PickedPhoto]) {
        guard !photos.isEmpty else {
            selectedPhotos = []
            state = isRoomFull ? .roomFull : .ready
            return
        }
        selectedPhotos = Array(photos.prefix(remainingCapacity))
        state = .selected(selectedPhotos)
    }

    public func reset() {
        selectedPhotos = []
        state = isRoomFull ? .roomFull : .ready
    }

    public func commit() async {
        guard case .selected = state, !selectedPhotos.isEmpty else { return }
        let batch = selectedPhotos
        photoSlotCountBeforeCommit = photoSlots.count
        state = .uploading(completed: 0, total: batch.count)

        do {
            let outcome = try await uploader.execute(
                accessToken: accessToken,
                roomID: room.id,
                photos: batch,
                onProgress: { [weak self] completed, total in
                    Task { @MainActor [weak self] in
                        guard let self, case .uploading = self.state else { return }
                        self.state = .uploading(completed: completed, total: total)
                    }
                }
            )
            photoSlots = outcome.photoSlots
            let failedIDs = Set(outcome.failures.map(\.pickedPhotoID))
            selectedPhotos = batch.filter { failedIDs.contains($0.id) }
            state = .committed(outcome)
        } catch {
            state = .commitFailed
        }
    }

    public func retry() async {
        switch state {
        case .committed, .commitFailed:
            guard !selectedPhotos.isEmpty else { return }
            state = .selected(selectedPhotos)
            await commit()
        default:
            return
        }
    }

    // MARK: - Slot assignment (engine, unchanged)

    public var projectedLayout: [LogicalPhotoSlot] {
        RoomPhotoSlotLayout.slots(forPhotoCount: projectedPhotoCount)
    }

    public var newPhotoAssignments: [(photo: PickedPhoto, slot: LogicalPhotoSlot)] {
        let layout = projectedLayout
        guard layout.count == projectedPhotoCount else { return [] }
        return zip(selectedPhotos, layout.dropFirst(existingPhotoCount)).map { ($0, $1) }
    }

    public var existingPhotosWillReflow: Bool {
        guard existingPhotoCount > 0, !selectedPhotos.isEmpty else { return false }
        let before = RoomPhotoSlotLayout.slots(forPhotoCount: existingPhotoCount)
        let after = projectedLayout
        return zip(before, after).contains { $0.anchor != $1.anchor }
    }

    // MARK: - Copy

    public var counterText: String {
        "\(projectedPhotoCount) / \(Room.maxPhotos) photos in this Room"
    }

    public var confirmActionTitle: String {
        let count = selectedPhotos.count
        return count == 1 ? "Add 1 Photo" : "Add \(count) Photos"
    }

    public var addPhotosActionTitle: String {
        existingPhotoCount == 0 ? "Choose Photos" : "Add More Photos"
    }

    public var roomFullMessage: String {
        "This Room is full — it holds all \(Room.maxPhotos) photographs. "
            + "Remove one to add another."
    }

    public var capacityMessage: String {
        remainingCapacity == 1
            ? "You can add 1 more photograph."
            : "You can add up to \(remainingCapacity) more photographs."
    }

    public var failedPhotoCount: Int {
        selectedPhotos.filter { !$0.didLoad }.count
    }

    public var failureNotice: String? {
        guard failedPhotoCount > 0 else { return nil }
        return failedPhotoCount == 1
            ? "1 photograph couldn't be loaded — it may still be downloading from iCloud."
            : "\(failedPhotoCount) photographs couldn't be loaded — they may still be downloading from iCloud."
    }

    public var reflowNotice: String? {
        guard existingPhotosWillReflow else { return nil }
        return "Adding these will rearrange the photographs already in this Room, "
            + "so the layout stays balanced."
    }

    public func uploadingMessage(completed: Int, total: Int) -> String {
        "Uploading \(min(completed + 1, total)) of \(total)…"
    }

    public func committedMessage(_ outcome: PhotoUploadOutcome) -> String {
        let added = max(0, outcome.photoSlots.count - photoSlotCountBeforeCommit)
        let addedText = added == 1 ? "1 photograph" : "\(added) photographs"
        if outcome.hasFailures {
            let failed = outcome.failures.count
            return "Added \(addedText) to \(room.name). "
                + (failed == 1 ? "1 photograph couldn't be saved." : "\(failed) photographs couldn't be saved.")
        }
        return "Added \(addedText) to \(room.name)."
    }

    public static let commitFailedMessage =
        "These photographs couldn't be saved. Check your connection and try again — nothing has been lost."

    public var retryActionTitle: String {
        let count = selectedPhotos.count
        return count == 1 ? "Retry 1 Photo" : "Retry \(count) Photos"
    }

    private var photoSlotCountBeforeCommit = 0
}
