import Foundation

@MainActor
public final class CollectionRoomCreationViewModel {

    public enum State: Equatable {
        case loadingCategories
        case ready(categories: [CollectionCategory], selectedCategoryID: String?)
        case noCategoriesAvailable
        case categoriesFailed(message: String)
    }

    public private(set) var state: State = .loadingCategories {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public private(set) var nameValidationMessage: String?

    public private(set) var creationErrorMessage: String?

    public private(set) var name: String = ""
    public private(set) var isCreating = false

    private let catalog: any CollectionCatalogServicing
    private let collections: any CollectionServicing
    private let accessToken: String
    private let analytics: any AnalyticsRecording

    public init(
        catalog: any CollectionCatalogServicing,
        collections: any CollectionServicing,
        accessToken: String,
        analytics: any AnalyticsRecording = NoAnalytics()
    ) {
        self.analytics = analytics
        self.catalog = catalog
        self.collections = collections
        self.accessToken = accessToken
    }

    // MARK: - Categories

    public func loadCategories() async {
        state = .loadingCategories
        do {
            let categories = try await catalog.fetchCollectionCategories(accessToken: accessToken)
            state = categories.isEmpty
                ? .noCategoriesAvailable
                : .ready(categories: categories, selectedCategoryID: selectedCategoryID)
        } catch {
            state = .categoriesFailed(
                message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load collection categories. Check your connection and try again.")
            )
        }
    }

    public func selectCategory(id: String) {
        guard case .ready(let categories, _) = state else { return }
        guard categories.contains(where: { $0.id == id }) else { return }
        creationErrorMessage = nil
        state = .ready(categories: categories, selectedCategoryID: id)
        analytics.record(.collectionRoomCreationStep(.categoryChosen, categoryID: id))
    }

    public var availableCategories: [CollectionCategory] {
        if case .ready(let categories, _) = state { return categories }
        return []
    }

    public var selectedCategoryID: String? {
        if case .ready(_, let selected) = state { return selected }
        return nil
    }

    // MARK: - Name

    public func updateName(_ newName: String) {
        name = newName
        if nameValidationMessage != nil, CollectionRoomNamingRules.rejection(for: newName) == nil {
            nameValidationMessage = nil
            onStateChange?(state)
        }
    }

    public var characterCountText: String {
        "\(name.count)/\(CollectionRoomNamingRules.interimMaximumLength)"
    }

    public var isOverLengthLimit: Bool {
        name.count > CollectionRoomNamingRules.interimMaximumLength
    }

    public var canCreate: Bool {
        !isCreating
            && selectedCategoryID != nil
            && CollectionRoomNamingRules.rejection(for: name) == nil
    }

    // MARK: - Creation

    public func createCollectionRoom() async -> CollectionRoom? {
        creationErrorMessage = nil
        analytics.record(.collectionRoomCreationStep(.createSubmitted, categoryID: selectedCategoryID))

        if let rejection = CollectionRoomNamingRules.rejection(for: name) {
            nameValidationMessage = CollectionRoomNamingRules.message(for: rejection)
            onStateChange?(state)
            return nil
        }
        guard let categoryID = selectedCategoryID else {
            creationErrorMessage = "Choose what you collect to continue."
            onStateChange?(state)
            return nil
        }

        nameValidationMessage = nil
        isCreating = true
        onStateChange?(state)
        defer {
            isCreating = false
            onStateChange?(state)
        }

        do {
            return try await collections.createCollectionRoom(
                accessToken: accessToken,
                name: name.trimmingCharacters(in: .whitespacesAndNewlines),
                categoryID: categoryID,
                designID: nil
            )
        } catch let error as CollectionAPIError {
            creationErrorMessage = message(for: error)
            if error.isUnknownCategory {
                await loadCategories()
            }
            return nil
        } catch {
            creationErrorMessage = NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't create your Collection Room. Please try again.")
            analytics.record(.failureShown(
                surface: .collectionRoomCreation, classification: .of(error),
                retried: false, retrySucceeded: false))
            return nil
        }
    }

    private func message(for error: CollectionAPIError) -> String {
        if error.isUnknownCategory {
            return "That category isn't available any more. Pick one from the refreshed list."
        }
        if error.isCategoryRequired {
            return "Choose what you collect to continue."
        }
        if error.isNameProblem {
            return "That name can't be used. Try a different one."
        }
        return "Couldn't create your Collection Room. Please try again."
    }

    // MARK: - Copy

    public static let noCategoriesMessage =
        "More collection categories are coming soon. There's nothing to start a Collection Room with just yet."

    public static let collectionRoomCountCap: Int? = nil
}
