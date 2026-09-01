import Foundation

public struct CollectionDesign: Equatable, Sendable, Identifiable {
    public let id: String

    public let displayName: String

    public let categoryID: String?

    public let isDevelopmentFixture: Bool

    public let assetBundle: AssetBundleRef

    public let sortOrder: Int

    public let tierCount: Int

    public init(
        id: String,
        displayName: String,
        categoryID: String? = nil,
        isDevelopmentFixture: Bool = false,
        assetBundle: AssetBundleRef,
        sortOrder: Int = 0,
        tierCount: Int = 1
    ) {
        self.id = id
        self.displayName = displayName
        self.categoryID = categoryID
        self.isDevelopmentFixture = isDevelopmentFixture
        self.assetBundle = assetBundle
        self.sortOrder = sortOrder
        self.tierCount = tierCount
    }

    public var isUniversal: Bool { categoryID == nil }

    public func applies(toCategoryID categoryID: String?) -> Bool {
        guard let scope = self.categoryID else { return true }
        return scope == categoryID
    }
}

public extension CollectionDesign {
    var previewSubject: PreviewSubject {
        PreviewSubject(id: id, displayName: displayName, assetBundle: assetBundle)
    }
}
