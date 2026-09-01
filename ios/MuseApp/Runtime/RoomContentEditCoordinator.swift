import Foundation

@MainActor
public final class RoomContentEditCoordinator {
    public enum Status: Equatable {
        case idle
        case saving
        case saved
        case failedTransport
        case failedUncertain
        case failedStale
        case failedInvalid
    }

    public private(set) var room: Room
    public private(set) var placements: [ResolvedPhotoPlacement]
    public private(set) var status: Status = .idle

    public var onPlacementsChanged: (([ResolvedPhotoPlacement]) -> Void)?
    public var onStatusChanged: ((Status) -> Void)?
    public var onPhotoAssetReplaced: ((_ previousAssetID: String, _ replacementAssetID: String) -> Void)?
    public var onPhotoRemovalBegan: ((_ assetID: String) -> Void)?
    public var onPhotoRemovalCommitted: ((_ assetID: String) -> Void)?
    public var onPhotoRemovalReverted: ((_ assetID: String) -> Void)?

    private let slotTable: RoomVariantSlotTable
    private let photoService: any RoomPhotoServicing
    private let roomService: any MuseumServicing
    private let photoReplacer: (any RoomPhotoReplacing)?
    private let accessToken: String

    private var serverSlots: [PhotoSlotAssignment]
    private var serverSlotsRevision = 0
    private var revision = 0
    private var isSendScheduled = false
    private var hasPendingSend = false
    private var isActive = true
    private var captionsInFlight: [String: (attempt: Int, caption: String)] = [:]
    private var captionAttempt = 0
    private var replacementsInFlight: Set<String> = []
    private var deletionsInFlight: Set<String> = []

    public init(
        room: Room,
        slotTable: RoomVariantSlotTable,
        placements: [ResolvedPhotoPlacement],
        photoService: any RoomPhotoServicing,
        roomService: any MuseumServicing,
        accessToken: String,
        photoReplacer: (any RoomPhotoReplacing)? = nil
    ) {
        let normalised = RoomPhotoOrder.normalised(room.photoSlots)
        self.room = room.replacingPhotoSlots(normalised)
        self.serverSlots = normalised
        self.slotTable = slotTable
        self.placements = placements
        self.photoService = photoService
        self.roomService = roomService
        self.photoReplacer = photoReplacer
        self.accessToken = accessToken
    }

    public var isPersisting: Bool { isSendScheduled }

    // MARK: - The swap

    @discardableResult
    public func swap(from: Int, to: Int) -> Bool {
        guard isActive else { return false }
        let swapped = RoomPhotoOrder.swapping(room.photoSlots, from: from, to: to)
        guard swapped != RoomPhotoOrder.normalised(room.photoSlots) else {
            return false
        }

        let candidate = room.replacingPhotoSlots(swapped)
        guard case .resolved(let newPlacements) = RoomPlacementResolver.resolve(room: candidate, slotTable: slotTable) else {
            return false
        }

        room = candidate
        placements = newPlacements
        revision += 1
        onPlacementsChanged?(newPlacements)
        setStatus(.saving)
        queueSend()
        return true
    }

    // MARK: - Captions

    public func caption(forAssetID assetID: String) -> String? {
        RoomPhotoCaptions.caption(forAssetID: assetID, in: room.photoSlots)
    }

    public func setCaption(_ caption: String, forAssetID assetID: String) async -> CaptionRules.CaptionSaveOutcome {
        guard isActive else {
            return .failed(message: Self.failedCaptionMessage)
        }
        if let rejection = CaptionRules.rejection(for: caption) {
            return .rejected(message: CaptionRules.message(for: rejection))
        }
        guard let current = RoomPhotoCaptions.caption(forAssetID: assetID, in: room.photoSlots) else {
            return .rejected(message: Self.photographGoneMessage)
        }

        let normalised = CaptionRules.normalised(caption)
        if current == normalised {
            return .saved
        }

        let attempt = captionAttempt + 1
        captionAttempt = attempt
        captionsInFlight[assetID] = (attempt, normalised)
        adopt(RoomPhotoCaptions.setting(normalised, forAssetID: assetID, in: room.photoSlots))

        do {
            let authoritative = try await photoService.setPhotoCaption(
                accessToken: accessToken,
                roomID: room.id,
                photoAssetID: assetID,
                caption: normalised
            )
            guard isActive else { return .saved }
            finishCaption(assetID: assetID, attempt: attempt, confirmed: authoritative)
            return .saved
        } catch let error as PhotoAPIError {
            guard isActive else { return .failed(message: Self.failedCaptionMessage) }
            finishCaption(assetID: assetID, attempt: attempt, confirmed: nil)
            switch error.code {
            case "photo_not_in_room":
                return .failed(message: Self.photographGoneMessage)
            case "caption_too_long", "invalid_caption":
                return .failed(message: error.message ?? Self.failedCaptionMessage)
            default:
                return .failed(message: Self.failedCaptionMessage)
            }
        } catch {
            guard isActive else { return .failed(message: Self.failedCaptionMessage) }
            finishCaption(assetID: assetID, attempt: attempt, confirmed: nil)
            return .failed(message: Self.failedCaptionMessage)
        }
    }

    private func finishCaption(assetID: String, attempt: Int, confirmed: [PhotoSlotAssignment]?) {
        let serverValue: String? = confirmed.flatMap {
            RoomPhotoCaptions.caption(forAssetID: assetID, in: $0)
        }
        if let serverValue {
            serverSlots = RoomPhotoCaptions.setting(serverValue, forAssetID: assetID, in: serverSlots)
        }

        guard captionsInFlight[assetID]?.attempt == attempt else {
            return
        }
        captionsInFlight.removeValue(forKey: assetID)

        let target = serverValue ?? RoomPhotoCaptions.caption(forAssetID: assetID, in: serverSlots) ?? ""
        guard RoomPhotoCaptions.caption(forAssetID: assetID, in: room.photoSlots) != target else { return }
        adopt(RoomPhotoCaptions.setting(target, forAssetID: assetID, in: room.photoSlots))
    }

    // MARK: - Replacement

    public var isReplacementAvailable: Bool { photoReplacer != nil }

    public func isReplacing(assetID: String) -> Bool { replacementsInFlight.contains(assetID) }

    public func replacePhoto(assetID: String, with photo: PickedPhoto) async -> PhotoReplacementOutcome {
        guard isActive, let photoReplacer else {
            return .rejected(message: Self.replacementUnavailableMessage)
        }
        guard RoomPhotoCaptions.caption(forAssetID: assetID, in: room.photoSlots) != nil else {
            return .rejected(message: Self.photographGoneMessage)
        }
        guard !replacementsInFlight.contains(assetID) else {
            return .rejected(message: Self.replacementInProgressMessage)
        }
        guard !deletionsInFlight.contains(assetID) else {
            return .rejected(message: Self.deletionInProgressMessage)
        }
        guard photo.normalizedFile != nil else {
            return .failed(.notNormalized)
        }

        replacementsInFlight.insert(assetID)
        defer { replacementsInFlight.remove(assetID) }

        let outcome = await photoReplacer.replacePhoto(
            accessToken: accessToken,
            roomID: room.id,
            photoAssetID: assetID,
            with: photo
        )
        guard isActive else { return outcome }

        if case .replaced(let authoritative, let replacementAssetID) = outcome {
            revision += 1
            confirmBaseline(RoomPhotoOrder.normalised(authoritative), atRevision: revision)
            onPhotoAssetReplaced?(assetID, replacementAssetID)
            var local = RoomPhotoReplacement.replacing(assetID, with: replacementAssetID, in: room.photoSlots)
            if let serverCaption = RoomPhotoCaptions.caption(forAssetID: replacementAssetID, in: authoritative) {
                local = RoomPhotoCaptions.setting(serverCaption, forAssetID: replacementAssetID, in: local)
            }
            adopt(local)
        }
        return outcome
    }

    // MARK: - Deletion

    public func isDeleting(assetID: String) -> Bool { deletionsInFlight.contains(assetID) }

    public func deletePhoto(assetID: String) async -> PhotoDeletionOutcome {
        guard isActive else {
            return .rejected(message: Self.deletionUnavailableMessage)
        }
        guard RoomPhotoCaptions.caption(forAssetID: assetID, in: room.photoSlots) != nil else {
            return .rejected(message: Self.photographGoneMessage)
        }
        guard !deletionsInFlight.contains(assetID) else {
            return .rejected(message: Self.deletionInProgressMessage)
        }
        guard !replacementsInFlight.contains(assetID) else {
            return .rejected(message: Self.replacementInProgressMessage)
        }

        deletionsInFlight.insert(assetID)
        revision += 1
        let sentRevision = revision
        onPhotoRemovalBegan?(assetID)
        adopt(RoomPhotoDeletion.removing(assetID, from: room.photoSlots))

        do {
            let authoritative = try await photoService.deletePhoto(
                accessToken: accessToken,
                roomID: room.id,
                photoAssetID: assetID
            )
            guard isActive else { return .deleted }
            finishDeletion(assetID: assetID, confirmed: RoomPhotoOrder.normalised(authoritative), sentRevision: sentRevision)
            return .deleted
        } catch let error as PhotoAPIError where error.code == "photo_not_in_room" {
            guard isActive else { return .deleted }
            deletionsInFlight.remove(assetID)
            onPhotoRemovalCommitted?(assetID)
            await reloadAuthoritativeRoom()
            return .deleted
        } catch {
            guard isActive else { return .failed(message: Self.deletionFailedMessage) }
            deletionsInFlight.remove(assetID)
            onPhotoRemovalReverted?(assetID)
            adopt(serverSlots)
            return .failed(message: NetworkFailureCopy.mutationOutcome(
                for: error,
                certainlyUnchanged: Self.deletionFailedMessage,
                possiblyApplied: "Couldn't confirm the deletion. \(NetworkFailureCopy.outcomeUnknownTail)"
            ))
        }
    }

    private func finishDeletion(assetID: String, confirmed: [PhotoSlotAssignment], sentRevision: Int) {
        confirmBaseline(confirmed, atRevision: sentRevision)
        deletionsInFlight.remove(assetID)
        onPhotoRemovalCommitted?(assetID)
        guard !isSendScheduled, sentRevision == revision else { return }
        if preservingInFlightEdits(confirmed) != RoomPhotoOrder.normalised(room.photoSlots) {
            adopt(confirmed)
        }
    }

    private func confirmBaseline(_ slots: [PhotoSlotAssignment], atRevision sentRevision: Int) {
        guard sentRevision >= serverSlotsRevision else { return }
        serverSlots = slots
        serverSlotsRevision = sentRevision
    }

    public func deactivate() {
        isActive = false
        hasPendingSend = false
        captionsInFlight.removeAll()
        replacementsInFlight.removeAll()
        deletionsInFlight.removeAll()
        onPlacementsChanged = nil
        onStatusChanged = nil
        onPhotoAssetReplaced = nil
        onPhotoRemovalBegan = nil
        onPhotoRemovalCommitted = nil
        onPhotoRemovalReverted = nil
    }

    // MARK: - Persistence

    private func queueSend() {
        guard !isSendScheduled else {
            hasPendingSend = true
            return
        }
        isSendScheduled = true
        Task { await sendLoop() }
    }

    private func sendLoop() async {
        defer { isSendScheduled = false }

        repeat {
            hasPendingSend = false
            let sentRevision = revision
            let order = RoomPhotoOrder.assetIDs(room.photoSlots)

            do {
                let authoritative = try await photoService.reorderPhotos(
                    accessToken: accessToken,
                    roomID: room.id,
                    orderedAssetIDs: order
                )
                applySuccess(authoritative, sentRevision: sentRevision)
            } catch let error as PhotoAPIError {
                await applyFailure(error)
                return
            } catch {
                applyRollback(status: NetworkResilience.requestCertainlyNotDelivered(error)
                    ? .failedTransport
                    : .failedUncertain)
                return
            }
        } while hasPendingSend
    }

    private func applySuccess(_ authoritative: [PhotoSlotAssignment], sentRevision: Int) {
        let normalised = RoomPhotoOrder.normalised(authoritative)
        confirmBaseline(normalised, atRevision: sentRevision)

        guard sentRevision == revision else {
            if !hasPendingSend {
                setStatus(.saved)
            }
            return
        }

        if preservingInFlightEdits(normalised) != RoomPhotoOrder.normalised(room.photoSlots) {
            adopt(normalised)
        }
        setStatus(.saved)
    }

    private func applyFailure(_ error: PhotoAPIError) async {
        switch error.code {
        case "order_mismatch":
            applyRollback(status: .failedStale)
            await reloadAuthoritativeRoom()
        case "invalid_order":
            applyRollback(status: .failedInvalid)
        default:
            applyRollback(status: .failedTransport)
        }
    }

    private func applyRollback(status: Status) {
        hasPendingSend = false
        if RoomPhotoOrder.normalised(room.photoSlots) != preservingInFlightEdits(serverSlots) {
            adopt(serverSlots)
        }
        setStatus(status)
    }

    private func reloadAuthoritativeRoom() async {
        do {
            let reloaded = try await roomService.fetchRoom(accessToken: accessToken, roomID: room.id)
            let normalised = RoomPhotoOrder.normalised(reloaded.photoSlots)
            confirmBaseline(normalised, atRevision: revision)
            adopt(normalised)
        } catch {
        }
    }

    private func adopt(_ slots: [PhotoSlotAssignment]) {
        let candidate = room.replacingPhotoSlots(preservingInFlightEdits(slots))
        guard case .resolved(let newPlacements) = RoomPlacementResolver.resolve(room: candidate, slotTable: slotTable) else {
            return
        }
        room = candidate
        placements = newPlacements
        onPlacementsChanged?(newPlacements)
    }

    private func preservingInFlightEdits(_ slots: [PhotoSlotAssignment]) -> [PhotoSlotAssignment] {
        guard !captionsInFlight.isEmpty || !deletionsInFlight.isEmpty else { return RoomPhotoOrder.normalised(slots) }
        var result = slots
        for assetID in deletionsInFlight {
            result = RoomPhotoDeletion.removing(assetID, from: result)
        }
        for (assetID, pending) in captionsInFlight {
            result = RoomPhotoCaptions.setting(pending.caption, forAssetID: assetID, in: result)
        }
        return RoomPhotoOrder.normalised(result)
    }

    private func setStatus(_ new: Status) {
        guard status != new else { return }
        status = new
        onStatusChanged?(new)
    }

    // MARK: - Copy

    public static let failedTransportMessage = "Couldn't save the new order. Your photographs are unchanged."
    public static let failedUncertainMessage =
        "Couldn't confirm the new order. \(NetworkFailureCopy.outcomeUnknownTail)"
    public static let failedCaptionMessage = "Couldn't save the caption. Your text is still here — try again."
    public static let photographGoneMessage = "That photograph is no longer in this Room."
    public static let failedStaleMessage = "This Room changed elsewhere. Showing its current order."
    public static let failedInvalidMessage = "Couldn't save the new order."
    public static let replacementUnavailableMessage = "Photos can't be replaced right now."
    public static let replacementInProgressMessage = "That photograph is already being replaced."
    public static let replacementCouldNotLoadMessage = "That photograph couldn't be loaded — it may still be downloading from iCloud. The previous one is unchanged."
    public static let replacementFailedMessage = "Couldn't replace the photograph. The previous one is unchanged."
    public static let deletionUnavailableMessage = "Photos can't be deleted right now."
    public static let deletionInProgressMessage = "That photograph is already being deleted."
    public static let deletionFailedMessage = "Couldn't delete the photograph. It's back where it was."

    public static func replacementFailureMessage(for failure: PhotoUploadFailure) -> String {
        switch failure {
        case .notNormalized:
            return replacementCouldNotLoadMessage
        case .rejectedDeclaration(let message):
            return message.map { "Couldn't replace the photograph: \($0). The previous one is unchanged." } ?? replacementFailedMessage
        case .rejectedAtCommit(let code) where code == "photo_not_in_room":
            return photographGoneMessage
        case .rejectedAtCommit, .transferFailed, .transport:
            return replacementFailedMessage
        case .transportOutcomeUnknown:
            return "Couldn't confirm the replacement. \(NetworkFailureCopy.outcomeUnknownTail)"
        }
    }

    public var statusMessage: String? {
        switch status {
        case .idle, .saving, .saved: return nil
        case .failedTransport: return Self.failedTransportMessage
        case .failedUncertain: return Self.failedUncertainMessage
        case .failedStale: return Self.failedStaleMessage
        case .failedInvalid: return Self.failedInvalidMessage
        }
    }
}
