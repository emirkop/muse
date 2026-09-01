import Foundation
import PhotosUI
import UIKit
import UniformTypeIdentifiers

@MainActor
public protocol PhotoPicking {
    func pickPhotos(limit: Int, presentingFrom viewController: UIViewController) async -> [PickedPhoto]
}

@MainActor
public final class SystemPhotoPicker: NSObject, PhotoPicking {
    private static let thumbnailMaxPixelSize = 512

    private let spool: PhotoUploadSpool
    private var continuation: CheckedContinuation<[PickedPhoto], Never>?

    public init(spool: PhotoUploadSpool = .shared) {
        self.spool = spool
        super.init()
    }

    public func pickPhotos(limit: Int, presentingFrom viewController: UIViewController) async -> [PickedPhoto] {
        guard limit > 0 else { return [] }

        var configuration = PHPickerConfiguration()
        configuration.filter = .images
        configuration.selectionLimit = limit
        configuration.preferredAssetRepresentationMode = .current
        configuration.selection = .ordered

        let picker = PHPickerViewController(configuration: configuration)
        picker.delegate = self

        return await withCheckedContinuation { continuation in
            self.continuation = continuation
            viewController.present(picker, animated: true)
        }
    }

    private func finish(with photos: [PickedPhoto]) {
        guard let continuation else { return }
        self.continuation = nil
        continuation.resume(returning: photos)
    }

    private static func load(_ results: [PHPickerResult], spool: PhotoUploadSpool) async -> [PickedPhoto] {
        try? spool.prepare()

        var photos: [PickedPhoto] = []
        photos.reserveCapacity(results.count)

        for result in results {
            let id = UUID().uuidString
            let state = await normalize(result.itemProvider, pickedPhotoID: id, spool: spool)
            photos.append(
                PickedPhoto(
                    id: id,
                    assetIdentifier: result.assetIdentifier,
                    loadState: state
                )
            )
        }

        return photos
    }

    private static func normalize(_ provider: NSItemProvider, pickedPhotoID: String, spool: PhotoUploadSpool) async -> PickedPhotoLoadState {
        guard let original = await loadOriginalData(from: provider) else { return .failed }
        let destination = spool.fileURL(forPickedPhotoID: pickedPhotoID)
        let thumbnailSize = thumbnailMaxPixelSize

        return await Task.detached(priority: .userInitiated) {
            do {
                let file = try PhotoNormalizer.normalize(original, to: destination)
                let normalizedBytes = try Data(contentsOf: destination, options: .mappedIfSafe)
                let thumbnail = try PhotoNormalizer.thumbnail(from: normalizedBytes, maxPixelSize: thumbnailSize)
                return PickedPhotoLoadState.ready(thumbnail: thumbnail, file: file)
            } catch {
                spool.remove(destination)
                return PickedPhotoLoadState.failed
            }
        }.value
    }

    private static func loadOriginalData(from provider: NSItemProvider) async -> Data? {
        let identifier = UTType.image.identifier
        guard provider.hasItemConformingToTypeIdentifier(identifier) else { return nil }

        return await withCheckedContinuation { continuation in
            provider.loadDataRepresentation(forTypeIdentifier: identifier) { data, _ in
                continuation.resume(returning: data)
            }
        }
    }
}

extension SystemPhotoPicker: PHPickerViewControllerDelegate {
    public func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
        picker.dismiss(animated: true)

        guard !results.isEmpty else {
            finish(with: [])
            return
        }

        let spool = self.spool
        Task { [weak self] in
            let photos = await Self.load(results, spool: spool)
            self?.finish(with: photos)
        }
    }
}
