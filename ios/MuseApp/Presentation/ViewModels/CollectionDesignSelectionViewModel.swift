import Foundation

@MainActor
public final class CollectionDesignSelectionViewModel {

    public enum State: Equatable {
        case loading
        case loaded(designs: [CollectionDesign], selectedDesignID: String?)
        case noDesignsAvailable
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public private(set) var room: CollectionRoom

    public private(set) var selectionErrorMessage: String?
    public private(set) var isSaving = false

    private let catalog: any CollectionCatalogServicing
    private let collections: any CollectionServicing
    private let accessToken: String

    public init(
        room: CollectionRoom,
        catalog: any CollectionCatalogServicing,
        collections: any CollectionServicing,
        accessToken: String
    ) {
        self.room = room
        self.catalog = catalog
        self.collections = collections
        self.accessToken = accessToken
    }

    public func load() async {
        state = .loading

        guard let categoryID = room.categoryID else {
            state = .failed(message: "This Collection Room has no category yet, so there are no designs to show.")
            return
        }

        do {
            let designs = try await catalog.fetchCollectionDesigns(
                accessToken: accessToken, categoryID: categoryID
            )
            state = designs.isEmpty
                ? .noDesignsAvailable
                : .loaded(designs: designs, selectedDesignID: room.designID)
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load collection designs. Check your connection and try again."))
        }
    }

    public var availableDesigns: [CollectionDesign] {
        if case .loaded(let designs, _) = state { return designs }
        return []
    }

    public var selectedDesignID: String? { room.designID }

    public func select(designID: String) async -> CollectionRoom? {
        selectionErrorMessage = nil

        guard availableDesigns.contains(where: { $0.id == designID }) else { return nil }

        isSaving = true
        onStateChange?(state)
        defer {
            isSaving = false
            onStateChange?(state)
        }

        do {
            room = try await collections.updateCollectionRoom(
                accessToken: accessToken,
                collectionRoomID: room.id,
                patch: .design(designID)
            )
            state = .loaded(designs: availableDesigns, selectedDesignID: room.designID)
            return room
        } catch let error as CollectionAPIError {
            selectionErrorMessage = message(for: error)
            if error.isDesignNotApplicable {
                await load()
            }
            return nil
        } catch {
            selectionErrorMessage = NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't apply that design. Please try again.")
            return nil
        }
    }

    private func message(for error: CollectionAPIError) -> String {
        if error.isDesignNotApplicable {
            return "That design isn't available for this collection any more. Pick one from the refreshed list."
        }
        return "Couldn't apply that design. Please try again."
    }

    // MARK: - Copy

    public static let noDesignsMessage =
        "Muse doesn't have any collection designs yet. They'll appear here once they're ready."

    public func subtitle(for design: CollectionDesign) -> String? {
        var parts: [String] = []
        if design.isDevelopmentFixture {
            parts.append("Development placeholder — not a finished design")
        } else if design.isUniversal {
            parts.append("Works with any collection")
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    public static let previewReassurance =
        "Changing the design changes how this Collection Room looks. Your items and their order stay exactly as they are."
}
