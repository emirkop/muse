import Foundation

public protocol CollectionItemRecognizing: Sendable {
    func recognize(_ input: RecognitionInput) async -> RecognitionOutcome
}

public struct RecognitionInput: Equatable, Sendable {
    public let imageFileURL: URL

    public let categoryID: String

    public init(imageFileURL: URL, categoryID: String) {
        self.imageFileURL = imageFileURL
        self.categoryID = categoryID
    }
}
