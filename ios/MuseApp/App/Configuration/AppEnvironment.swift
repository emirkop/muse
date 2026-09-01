import Foundation

public enum AppEnvironment {
    case development
    case staging
    case production

    public static var current: AppEnvironment {
        #if DEBUG
        return .development
        #else
        return .production
        #endif
    }

    public var shareLinkHosts: Set<String> {
        switch self {
        case .development:
            return ["muse.app", "localhost", "127.0.0.1"]
        case .staging, .production:
            return ["muse.app"]
        }
    }

    public var collectionCapacityProductID: String {
        "dev.muse.placeholder.collection_capacity"
    }

    public var apiBaseURL: URL {
        switch self {
        case .development:
            return URL(string: "http://localhost:8080")!
        case .staging, .production:
            return URL(string: "https://api.muse.app")!
        }
    }
}
