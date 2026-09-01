import Foundation
import Photos
import PhotosUI
import UIKit

public struct PhotoKitPermissionProvider: PhotoLibraryPermissionProviding {
    public init() {}

    public func currentAccess() async -> PhotoLibraryAccess {
        Self.map(PHPhotoLibrary.authorizationStatus(for: .readWrite))
    }

    public func requestAccess() async -> PhotoLibraryAccess {
        Self.map(await PHPhotoLibrary.requestAuthorization(for: .readWrite))
    }

    static func map(_ status: PHAuthorizationStatus) -> PhotoLibraryAccess {
        switch status {
        case .notDetermined: return .notDetermined
        case .restricted: return .restricted
        case .denied: return .denied
        case .authorized: return .fullAccess
        case .limited: return .limitedAccess
        @unknown default:
            return .denied
        }
    }
}

@MainActor
public protocol LimitedPhotoLibraryManaging {
    func presentLimitedLibraryPicker(from viewController: UIViewController)
}

@MainActor
public struct LimitedPhotoLibraryManager: LimitedPhotoLibraryManaging {
    public init() {}

    public func presentLimitedLibraryPicker(from viewController: UIViewController) {
        PHPhotoLibrary.shared().presentLimitedLibraryPicker(from: viewController)
    }
}

// MARK: -, CLOSED at — read this before wiring a caller
