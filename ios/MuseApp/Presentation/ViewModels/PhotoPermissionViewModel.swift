import Foundation

@MainActor
public final class PhotoPermissionViewModel {
    public enum State: Equatable {
        case checking
        case explaining
        case requesting
        case granted(PhotoLibraryAccess)
        case denied
        case restricted
    }

    public private(set) var state: State = .checking {
        didSet {
            guard state != oldValue else { return }
            onStateChange?(state)
        }
    }

    public var onStateChange: ((State) -> Void)?

    private let permissionProvider: PhotoLibraryPermissionProviding

    public init(permissionProvider: PhotoLibraryPermissionProviding) {
        self.permissionProvider = permissionProvider
    }

    public func start() async {
        state = .checking
        state = Self.entryState(for: await permissionProvider.currentAccess())
    }

    public func refresh() async {
        let access = await permissionProvider.currentAccess()
        if case .notDetermined = access, state == .denied || state == .restricted {
            return
        }
        state = Self.entryState(for: access)
    }

    public func requestAccess() async {
        guard state == .explaining else { return }
        state = .requesting
        state = Self.entryState(for: await permissionProvider.requestAccess())
    }

    private static func entryState(for access: PhotoLibraryAccess) -> State {
        switch access {
        case .notDetermined: return .explaining
        case .fullAccess: return .granted(.fullAccess)
        case .limitedAccess: return .granted(.limitedAccess)
        case .denied: return .denied
        case .restricted: return .restricted
        }
    }

    // MARK: - What the screen offers

    public var showsManageSelectedPhotos: Bool {
        state == .granted(.limitedAccess)
    }

    public var showsSettingsLink: Bool {
        state == .denied
    }

    public var allowsContinuingWithoutPhotos: Bool { true }

    public var canProceedToSelection: Bool {
        if case .granted(let access) = state { return access.allowsPhotoSelection }
        return false
    }

    // MARK: - Copy

    public static let explanationTitle = "Choose photos to display in your Room"
    public static let explanationBody =
        "Muse needs access to your photo library so you can pick the photographs that hang on your Room's walls. "
        + "Your photos stay yours — Muse only reads the ones you choose."
    public static let allowActionTitle = "Allow Access"

    public static let deniedTitle = "Photo access is off"
    public static let deniedBody =
        "Muse can't show your photographs without access to your library. "
        + "You can turn it on in Settings, or come back to this Room later."
    public static let settingsActionTitle = "Open Settings"

    public static let restrictedTitle = "Photo access isn't available"
    public static let restrictedBody =
        "Photo access is restricted on this device, so it can't be changed here. "
        + "You can still build this Room and add photographs later."

    public static let limitedTitle = "You've shared some photos"
    public static let limitedBody =
        "Muse can only see the photographs you selected. You can change that selection at any time."
    public static let manageSelectionActionTitle = "Manage Selected Photos"

    public static let fullAccessTitle = "Photo access granted"
    public static let fullAccessBody = "You can now choose photographs for this Room."

    public static let continueWithoutPhotosActionTitle = "Not Now"

    public static var photolessRoomNotice: String {
        RoomCreationViewModel.zeroPhotoRoomsGateNotice
    }
}
