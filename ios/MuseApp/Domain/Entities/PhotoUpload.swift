import Foundation

public struct NormalizedPhotoFile: Equatable, Sendable {
    public let fileURL: URL
    public let contentType: String
    public let byteSize: Int
    public let pixelWidth: Int
    public let pixelHeight: Int
    public let sha256Hex: String

    public init(fileURL: URL, contentType: String, byteSize: Int, pixelWidth: Int, pixelHeight: Int, sha256Hex: String) {
        self.fileURL = fileURL
        self.contentType = contentType
        self.byteSize = byteSize
        self.pixelWidth = pixelWidth
        self.pixelHeight = pixelHeight
        self.sha256Hex = sha256Hex
    }

    public static let maxLongEdge = 3072
    public static let contentType = "image/jpeg"
}

public struct PhotoUploadDeclaration: Equatable, Sendable {
    public let clientUploadID: String
    public let contentType: String
    public let byteSize: Int
    public let pixelWidth: Int
    public let pixelHeight: Int
    public let sha256Hex: String

    public init(clientUploadID: String, file: NormalizedPhotoFile) {
        self.clientUploadID = clientUploadID
        self.contentType = file.contentType
        self.byteSize = file.byteSize
        self.pixelWidth = file.pixelWidth
        self.pixelHeight = file.pixelHeight
        self.sha256Hex = file.sha256Hex
    }
}

public struct PhotoUploadTicket: Equatable, Sendable {
    public struct UploadInstructions: Equatable, Sendable {
        public let url: URL
        public let method: String
        public let headers: [String: String]
        public let expiresAt: Date

        public init(url: URL, method: String, headers: [String: String], expiresAt: Date) {
            self.url = url
            self.method = method
            self.headers = headers
            self.expiresAt = expiresAt
        }
    }

    public let assetID: String
    public let isCommitted: Bool
    public let upload: UploadInstructions?

    public init(assetID: String, isCommitted: Bool, upload: UploadInstructions?) {
        self.assetID = assetID
        self.isCommitted = isCommitted
        self.upload = upload
    }
}

public struct PhotoDownloadTicket: Equatable, Sendable {
    public let photoAssetID: String
    public let url: URL
    public let expiresAt: Date
    public let pixelWidth: Int
    public let pixelHeight: Int

    public init(photoAssetID: String, url: URL, expiresAt: Date, pixelWidth: Int, pixelHeight: Int) {
        self.photoAssetID = photoAssetID
        self.url = url
        self.expiresAt = expiresAt
        self.pixelWidth = pixelWidth
        self.pixelHeight = pixelHeight
    }
}

public enum PhotoUploadFailure: Error, Equatable, Sendable {
    case notNormalized
    case rejectedDeclaration(message: String?)
    case transferFailed
    case rejectedAtCommit(code: String?)
    case transport
    case transportOutcomeUnknown
}

public struct PhotoUploadOutcome: Equatable, Sendable {
    public struct Failure: Equatable, Sendable {
        public let pickedPhotoID: String
        public let reason: PhotoUploadFailure

        public init(pickedPhotoID: String, reason: PhotoUploadFailure) {
            self.pickedPhotoID = pickedPhotoID
            self.reason = reason
        }
    }

    public let photoSlots: [PhotoSlotAssignment]
    public let failures: [Failure]

    public init(photoSlots: [PhotoSlotAssignment], failures: [Failure]) {
        self.photoSlots = photoSlots
        self.failures = failures
    }

    public var succeededCount: Int { photoSlots.count }
    public var hasFailures: Bool { !failures.isEmpty }
}
