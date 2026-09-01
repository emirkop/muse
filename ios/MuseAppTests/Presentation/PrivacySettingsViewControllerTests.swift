import XCTest
@testable import MuseApp

@MainActor
final class PrivacySettingsViewControllerTests: XCTestCase {

    private func makeScreen(
        museumPrivacy: MusePrivacy,
        rooms: [Room]
    ) -> (PrivacySettingsViewController, PrivacySettingsViewModel, FakeMuseumService) {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: "style_modern", privacy: museumPrivacy))
        service.roomsResult = .success(rooms)
        let viewModel = PrivacySettingsViewModel(museumService: service, accessToken: "token")
        let viewController = PrivacySettingsViewController(viewModel: viewModel)
        viewController.loadViewIfNeeded()
        return (viewController, viewModel, service)
    }

    func test_screen_showsASwitchPerLevel_reflectingTheServerState() async {
        let rooms = [
            Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .public),
            Room(id: "r2", name: "Study", variantID: "v2", privacy: .private)
        ]
        let (viewController, viewModel, _) = makeScreen(museumPrivacy: .public, rooms: rooms)

        await viewModel.load()

        XCTAssertEqual(viewController.testSwitchStates["Museum privacy"], true)
        XCTAssertEqual(viewController.testSwitchStates["The Long Hall privacy"], true)
        XCTAssertEqual(viewController.testSwitchStates["Study privacy"], false)
    }

    func test_publicRoomInsideAPrivateMuseum_isShownAsHiddenWithTheReason() async {
        let rooms = [Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .public)]
        let (viewController, viewModel, _) = makeScreen(museumPrivacy: .private, rooms: rooms)

        await viewModel.load()

        XCTAssertEqual(viewController.testSwitchStates["Museum privacy"], false)
        XCTAssertEqual(viewController.testSwitchStates["The Long Hall privacy"], true,
                       "the Room's own state is still shown truthfully")
        XCTAssertTrue(
            viewController.testVisibleText.contains("Hidden from visitors — your Museum is Private."),
            "the screen must explain the Museum-level override: \(viewController.testVisibleText)"
        )
        XCTAssertTrue(
            viewController.testVisibleText.contains(
                "Your Museum is Private — no one can enter, regardless of individual Room settings."),
            viewController.testVisibleText.description
        )
    }

    func test_screen_withNoRooms_saysSoRatherThanShowingAnEmptySection() async {
        let (viewController, viewModel, _) = makeScreen(museumPrivacy: .private, rooms: [])

        await viewModel.load()

        XCTAssertTrue(viewController.testVisibleText.contains("Your Museum has no Rooms yet."),
                      viewController.testVisibleText.description)
        XCTAssertNil(viewController.testSwitchStates["The Long Hall privacy"])
    }

    func test_loadFailure_showsNoPrivacyControlsAtAll() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.invalidResponse)
        let viewModel = PrivacySettingsViewModel(museumService: service, accessToken: "token")
        let viewController = PrivacySettingsViewController(viewModel: viewModel)
        viewController.loadViewIfNeeded()

        await viewModel.load()

        XCTAssertTrue(viewController.testSwitchStates.isEmpty,
                      "a switch with a guessed state would be worse than no switch")
    }
}
