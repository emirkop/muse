import Foundation

@MainActor
public final class MuseumEntryViewModel {
    public enum State: Equatable {
        case loading
        case needsCreation
        case hasMuseum(Museum)
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let museumService: MuseumServicing
    private let accessToken: String
    private let refresh = RefreshCoordination()

    public private(set) var refreshFailureNotice: String?

    public init(museumService: MuseumServicing, accessToken: String) {
        self.museumService = museumService
        self.accessToken = accessToken
    }

    public func load() async {
        let token = refresh.begin()
        if !hasMuseum { state = .loading }
        do {
            let museum = try await museumService.fetchMuseum(accessToken: accessToken)
            guard refresh.isCurrent(token) else { return }
            refreshFailureNotice = nil
            state = .hasMuseum(museum)
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 {
            guard refresh.isCurrent(token) else { return }
            refreshFailureNotice = nil
            state = .needsCreation
        } catch {
            guard refresh.isCurrent(token) else { return }
            if hasMuseum {
                refreshFailureNotice = RefreshFailureNotice.message(for: error)
                onStateChange?(state)
            } else {
                refreshFailureNotice = nil
                state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load your Museum. Please try again."))
            }
        }
    }

    private var hasMuseum: Bool {
        if case .hasMuseum = state { return true }
        return false
    }

    public var canOfferCreation: Bool {
        if case .needsCreation = state { return true }
        return false
    }
}
