import Foundation

public protocol RoomPhotoUploading: Sendable {
    func execute(
        accessToken: String,
        roomID: String,
        photos: [PickedPhoto],
        onProgress: UploadRoomPhotosUseCase.ProgressHandler?
    ) async throws -> PhotoUploadOutcome
}

extension UploadRoomPhotosUseCase: RoomPhotoUploading {}

public protocol RoomPhotoReplacing: Sendable {
    func replacePhoto(
        accessToken: String,
        roomID: String,
        photoAssetID: String,
        with photo: PickedPhoto
    ) async -> PhotoReplacementOutcome
}

extension UploadRoomPhotosUseCase: RoomPhotoReplacing {}

public final class UploadRoomPhotosUseCase: Sendable {
    public static let maxConcurrentUploads = 3

    private let photoService: RoomPhotoServicing
    private let uploader: ObjectUploading
    private let removeSpooledFile: @Sendable (URL) -> Void
    private let maxConcurrent: Int

    public init(
        photoService: RoomPhotoServicing,
        uploader: ObjectUploading,
        removeSpooledFile: @escaping @Sendable (URL) -> Void,
        maxConcurrent: Int = UploadRoomPhotosUseCase.maxConcurrentUploads
    ) {
        self.photoService = photoService
        self.uploader = uploader
        self.removeSpooledFile = removeSpooledFile
        self.maxConcurrent = max(1, maxConcurrent)
    }

    public typealias ProgressHandler = @Sendable (_ completed: Int, _ total: Int) -> Void

    public func execute(
        accessToken: String,
        roomID: String,
        photos: [PickedPhoto],
        onProgress: ProgressHandler? = nil
    ) async throws -> PhotoUploadOutcome {
        guard !photos.isEmpty else {
            return PhotoUploadOutcome(photoSlots: [], failures: [])
        }

        var uploaded: [Int: String] = [:]
        var failures: [Int: PhotoUploadFailure] = [:]
        let total = photos.count
        var completed = 0

        try await withThrowingTaskGroup(of: (Int, Result<String, PhotoUploadFailure>).self) { group in
            var nextIndex = 0

            func enqueue() {
                guard nextIndex < photos.count else { return }
                let index = nextIndex
                let photo = photos[index]
                nextIndex += 1
                group.addTask { [self] in
                    (index, await self.uploadOne(photo, accessToken: accessToken))
                }
            }

            for _ in 0..<min(maxConcurrent, photos.count) { enqueue() }

            for try await (index, result) in group {
                switch result {
                case .success(let assetID): uploaded[index] = assetID
                case .failure(let reason): failures[index] = reason
                }
                completed += 1
                onProgress?(completed, total)
                enqueue()
            }
        }

        let orderedIndices = uploaded.keys.sorted()
        let assetIDs = orderedIndices.map { uploaded[$0]! }

        var photoSlots: [PhotoSlotAssignment] = []
        if !assetIDs.isEmpty {
            do {
                photoSlots = try await photoService.assignPhotos(accessToken: accessToken, roomID: roomID, assetIDs: assetIDs)
            } catch let error as PhotoAPIError {
                if let offending = error.assetID, let index = orderedIndices.first(where: { uploaded[$0] == offending }) {
                    failures[index] = .rejectedAtCommit(code: error.code)
                    let remaining = orderedIndices.filter { $0 != index }.map { uploaded[$0]! }
                    if !remaining.isEmpty {
                        photoSlots = try await photoService.assignPhotos(accessToken: accessToken, roomID: roomID, assetIDs: remaining)
                    }
                } else {
                    throw error
                }
            }
        }

        for index in orderedIndices where failures[index] == nil {
            if let file = photos[index].normalizedFile {
                removeSpooledFile(file.fileURL)
            }
        }

        let orderedFailures = failures.keys.sorted().map {
            PhotoUploadOutcome.Failure(pickedPhotoID: photos[$0].id, reason: failures[$0]!)
        }
        return PhotoUploadOutcome(photoSlots: photoSlots, failures: orderedFailures)
    }

    // MARK: - Replacement

    public func replacePhoto(
        accessToken: String,
        roomID: String,
        photoAssetID: String,
        with photo: PickedPhoto
    ) async -> PhotoReplacementOutcome {
        let replacementAssetID: String
        switch await uploadOne(photo, accessToken: accessToken) {
        case .failure(let reason):
            return .failed(reason)
        case .success(let assetID):
            replacementAssetID = assetID
        }

        do {
            let slots = try await photoService.replacePhoto(
                accessToken: accessToken,
                roomID: roomID,
                photoAssetID: photoAssetID,
                replacementAssetID: replacementAssetID
            )
            if let file = photo.normalizedFile {
                removeSpooledFile(file.fileURL)
            }
            return .replaced(photoSlots: slots, replacementAssetID: replacementAssetID)
        } catch let error as PhotoAPIError where (400...499).contains(error.statusCode) {
            return .failed(.rejectedAtCommit(code: error.code))
        } catch {
            return .failed(NetworkResilience.requestCertainlyNotDelivered(error)
                ? .transport
                : .transportOutcomeUnknown)
        }
    }

    // MARK: - One photograph

    private func uploadOne(_ photo: PickedPhoto, accessToken: String) async -> Result<String, PhotoUploadFailure> {
        guard let file = photo.normalizedFile else {
            return .failure(.notNormalized)
        }

        let ticket: PhotoUploadTicket
        do {
            ticket = try await photoService.initiateUpload(
                accessToken: accessToken,
                declaration: PhotoUploadDeclaration(clientUploadID: photo.id, file: file)
            )
        } catch let error as PhotoAPIError where (400...499).contains(error.statusCode) {
            return .failure(.rejectedDeclaration(message: error.message))
        } catch {
            return .failure(.transport)
        }

        guard let instructions = ticket.upload else {
            return .success(ticket.assetID)
        }

        do {
            try await uploader.upload(file: file.fileURL, using: instructions)
        } catch {
            return .failure(.transferFailed)
        }
        return .success(ticket.assetID)
    }
}
