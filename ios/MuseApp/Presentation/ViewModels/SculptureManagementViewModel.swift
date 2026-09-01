import Foundation

@MainActor
public final class SculptureManagementViewModel {
    public enum State: Equatable {
        case loading
        case ready
        case failed(String)
    }

    public private(set) var state: State = .loading {
        didSet { onChange?() }
    }

    public private(set) var entries: [SculptureCatalogEntry] = []

    public private(set) var notice: String? {
        didSet { onChange?() }
    }

    public var onChange: (() -> Void)?

    private let catalog: any CatalogServicing
    private let accessToken: String

    public init(catalog: any CatalogServicing, accessToken: String) {
        self.catalog = catalog
        self.accessToken = accessToken
    }

    public func load() async {
        notice = nil
        do {
            entries = try await catalog.fetchSculptures(accessToken: accessToken)
            state = .ready
        } catch {
            state = .failed(NetworkFailureCopy.message(
                for: error, operation: .read, otherwise: Self.catalogFailedMessage))
        }
    }

    public func reload() async {
        state = .loading
        await load()
    }

    public func show(notice: String?) {
        self.notice = notice
    }

    public func displayName(forCatalogID catalogID: String) -> String {
        entries.first { $0.id == catalogID }?.displayName ?? catalogID
    }

    public static let emptyCatalogMessage =
        "No sculptures are available yet. They'll appear here once Muse's sculpture collection is ready."
    public static let catalogFailedMessage =
        "Couldn't load the sculpture catalog. Check your connection and try again."
}
