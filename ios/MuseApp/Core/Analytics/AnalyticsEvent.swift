import Foundation

public enum AnalyticsEvent: Equatable, Sendable {

    public enum MuseumCreationStep: String, Sendable {
        case styleListShown = "style_list_shown"
        case stylePreviewed = "style_previewed"
        case styleConfirmed = "style_confirmed"
    }

    public enum RoomCreationStep: String, Sendable {
        case nameEntered = "name_entered"
        case variantListShown = "variant_list_shown"
        case variantPreviewed = "variant_previewed"
        case variantConfirmed = "variant_confirmed"
    }

    public enum CollectionRoomCreationStep: String, Sendable {
        case categoryListShown = "category_list_shown"
        case categoryChosen = "category_chosen"
        case nameEntered = "name_entered"
        case createSubmitted = "create_submitted"
    }

    public enum SearchOutcome: String, Sendable {
        case selected
        case abandoned
    }

    public enum CapacityUpgradeStep: String, Sendable {
        case capacityScreenShown = "capacity_screen_shown"
        case purchaseStarted = "purchase_started"
        case purchaseFailed = "purchase_failed"
    }

    public enum FailureSurface: String, Sendable {
        case authentication
        case profile
        case avatarSelection = "avatar_selection"
        case museumEntry = "museum_entry"
        case roomList = "room_list"
        case styleSelection = "style_selection"
        case variantSelection = "variant_selection"
        case roomEntry = "room_entry"
        case photoUpload = "photo_upload"
        case collectionRoomList = "collection_room_list"
        case collectionRoomCreation = "collection_room_creation"
        case collectionDesignSelection = "collection_design_selection"
        case catalogSearch = "catalog_search"
        case collectionItemAdd = "collection_item_add"
        case sharing
        case music
        case capacity
        case launch
    }

    public enum FailureClassification: String, Sendable {
        case offline
        case unreachable
        case server
        case content

        public static func of(_ error: Error) -> FailureClassification {
            switch NetworkResilience.classify(error) {
            case .offline: return .offline
            case .unreachable: return .unreachable
            case .cancelled, .other: return .server
            }
        }
    }

    case museumCreationStep(MuseumCreationStep)
    case roomCreationStep(RoomCreationStep)
    case collectionRoomCreationStep(CollectionRoomCreationStep, categoryID: String?)
    case catalogSearchOutcome(SearchOutcome, categoryID: String)
    case capacityUpgradeStep(CapacityUpgradeStep)
    case failureShown(surface: FailureSurface, classification: FailureClassification,
                      retried: Bool, retrySucceeded: Bool)

    // MARK: - Wire form

    var name: String {
        switch self {
        case .museumCreationStep: return "museum_creation_step"
        case .roomCreationStep: return "room_creation_step"
        case .collectionRoomCreationStep: return "collection_room_creation_step"
        case .catalogSearchOutcome: return "catalog_search_outcome"
        case .capacityUpgradeStep: return "capacity_upgrade_step"
        case .failureShown: return "failure_shown"
        }
    }

    func payload(uuid: String) -> AnalyticsEventPayload {
        var payload = AnalyticsEventPayload(eventUUID: uuid, name: name)
        switch self {
        case .museumCreationStep(let step):
            payload.step = step.rawValue
        case .roomCreationStep(let step):
            payload.step = step.rawValue
        case .collectionRoomCreationStep(let step, let categoryID):
            payload.step = step.rawValue
            payload.categoryID = categoryID
        case .catalogSearchOutcome(let outcome, let categoryID):
            payload.outcome = outcome.rawValue
            payload.categoryID = categoryID
        case .capacityUpgradeStep(let step):
            payload.step = step.rawValue
        case .failureShown(let surface, let classification, let retried, let retrySucceeded):
            payload.surface = surface.rawValue
            payload.classification = classification.rawValue
            payload.retried = retried
            payload.retrySucceeded = retrySucceeded
        }
        return payload
    }
}

public struct AnalyticsEventPayload: Encodable, Equatable, Sendable {
    public let eventUUID: String
    public let name: String
    public var step: String?
    public var categoryID: String?
    public var resultBucket: String?
    public var outcome: String?
    public var reason: String?
    public var surface: String?
    public var classification: String?
    public var retried: Bool?
    public var retrySucceeded: Bool?

    enum CodingKeys: String, CodingKey {
        case eventUUID = "event_uuid"
        case name
        case step
        case categoryID = "category_id"
        case resultBucket = "result_bucket"
        case outcome
        case reason
        case surface
        case classification
        case retried
        case retrySucceeded = "retry_succeeded"
    }
}
