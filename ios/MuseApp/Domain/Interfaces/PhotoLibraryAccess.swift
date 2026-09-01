import Foundation

public enum PhotoLibraryAccess: Equatable, Sendable {
    case notDetermined
    case fullAccess
    case limitedAccess
    case denied
    case restricted

    public var allowsPhotoSelection: Bool {
        switch self {
        case .fullAccess, .limitedAccess: return true
        case .notDetermined, .denied, .restricted: return false
        }
    }

    public var canPresentSystemPrompt: Bool {
        self == .notDetermined
    }

    public var isResolvableInSettings: Bool {
        self == .denied
    }
}

public protocol PhotoLibraryPermissionProviding: Sendable {
    func currentAccess() async -> PhotoLibraryAccess

    func requestAccess() async -> PhotoLibraryAccess
}
