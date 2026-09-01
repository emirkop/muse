import Foundation

@MainActor
public final class PrivacySettingsViewModel {
    public struct RoomRow: Equatable, Sendable {
        public let room: Room
        public let visibility: RoomVisitorVisibility
        public let isApplying: Bool

        public var isPublic: Bool { room.privacy == .public }
        public var stateLabel: String { room.privacy == .public ? "Public" : "Private" }

        public var visibilityDescription: String {
            switch visibility {
            case .visible:
                return "Visitors can see this Room."
            case .hiddenByRoom:
                return "Hidden from visitors."
            case .hiddenByMuseum:
                return "Hidden from visitors — your Museum is Private."
            }
        }
    }

    public struct Content: Equatable, Sendable {
        public let museumPrivacy: MusePrivacy
        public let museumIsApplying: Bool
        public let rooms: [RoomRow]
        public let notice: String?

        public var museumIsPublic: Bool { museumPrivacy == .public }
        public var museumStateLabel: String { museumPrivacy == .public ? "Public" : "Private" }

        public var museumDescription: String {
            museumPrivacy == .public
                ? "Your Museum is Public — visitors can enter and see its Public Rooms."
                : "Your Museum is Private — no one can enter, regardless of individual Room settings."
        }
    }

    public struct ExposureConfirmation: Equatable, Sendable {
        public let title: String
        public let message: String
        public let confirmTitle: String
    }

    public enum State: Equatable {
        case loading
        case loaded(Content)
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let museumService: MuseumServicing
    private let accessToken: String

    private var museum: Museum?
    private var rooms: [Room] = []
    private var museumIsApplying = false
    private var applyingRoomIDs: Set<String> = []
    private var notice: String?

    private let refresh = RefreshCoordination()

    public init(museumService: MuseumServicing, accessToken: String) {
        self.museumService = museumService
        self.accessToken = accessToken
    }

    // MARK: - Loading

    public func load() async {
        let token = refresh.begin()
        state = .loading
        notice = nil
        do {
            let loadedMuseum = try await museumService.fetchMuseum(accessToken: accessToken)
            let loadedRooms = try await museumService.listRooms(accessToken: accessToken)
            guard refresh.isCurrent(token) else { return }
            museum = loadedMuseum
            rooms = loadedRooms
            publish()
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 {
            guard refresh.isCurrent(token) else { return }
            state = .failed(message: "You haven't created your Museum yet.")
        } catch {
            guard refresh.isCurrent(token) else { return }
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load your privacy settings. Please try again."))
        }
    }

    // MARK: - Confirmation

    public func confirmation(forMuseumTarget target: MusePrivacy) -> ExposureConfirmation? {
        guard let museum,
              MuseumPrivacyRules.museumChangeNeedsExposureConfirmation(from: museum.privacy, to: target)
        else { return nil }

        let exposed = MuseumPrivacyRules.roomsExposedByMakingMuseumPublic(rooms)
        let detail: String
        switch exposed {
        case 0:
            detail = "Visitors will be able to enter. None of your Rooms are Public yet, so they won't see any Room until you make one Public."
        case 1:
            detail = "Visitors will be able to enter, and 1 Public Room will become visible to them."
        default:
            detail = "Visitors will be able to enter, and \(exposed) Public Rooms will become visible to them."
        }
        return ExposureConfirmation(title: "Make your Museum Public?", message: detail, confirmTitle: "Make Public")
    }

    public func confirmation(forRoom room: Room, target: MusePrivacy) -> ExposureConfirmation? {
        guard let museum,
              MuseumPrivacyRules.roomChangeNeedsExposureConfirmation(
                  museum: museum.privacy, from: room.privacy, to: target)
        else { return nil }

        return ExposureConfirmation(
            title: "Make “\(room.name)” Public?",
            message: "Your Museum is Public, so visitors will be able to enter this Room.",
            confirmTitle: "Make Public"
        )
    }

    // MARK: - Mutations

    public func setMuseumPrivacy(_ target: MusePrivacy) async {
        guard let current = museum, current.privacy != target, !museumIsApplying else { return }
        museumIsApplying = true
        notice = nil
        publish()

        do {
            museum = try await museumService.changePrivacy(accessToken: accessToken, privacy: target)
        } catch {
            notice = "Couldn't change your Museum's privacy. It's still \(label(current.privacy))."
        }
        museumIsApplying = false
        publish()
    }

    public func setPrivacy(_ target: MusePrivacy, forRoomWithID roomID: String) async {
        guard museum != nil,
              let index = rooms.firstIndex(where: { $0.id == roomID }),
              rooms[index].privacy != target,
              !applyingRoomIDs.contains(roomID)
        else { return }

        let previous = rooms[index]
        applyingRoomIDs.insert(roomID)
        notice = nil
        publish()

        do {
            let updated = try await museumService.updateRoom(
                accessToken: accessToken,
                roomID: roomID,
                patch: .privacy(target)
            )
            if let current = rooms.firstIndex(where: { $0.id == roomID }) {
                rooms[current] = updated
            }
        } catch {
            notice = "Couldn't change “\(previous.name)”. It's still \(label(previous.privacy))."
        }
        applyingRoomIDs.remove(roomID)
        publish()
    }

    // MARK: - Rendering

    private func label(_ privacy: MusePrivacy) -> String {
        privacy == .public ? "Public" : "Private"
    }

    private func publish() {
        guard let museum else { return }
        state = .loaded(Content(
            museumPrivacy: museum.privacy,
            museumIsApplying: museumIsApplying,
            rooms: rooms.map { room in
                RoomRow(
                    room: room,
                    visibility: MuseumPrivacyRules.visitorVisibility(
                        museum: museum.privacy, room: room.privacy),
                    isApplying: applyingRoomIDs.contains(room.id)
                )
            },
            notice: notice
        ))
    }
}
