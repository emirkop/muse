import RealityKit
import simd
import UIKit
import XCTest
@testable import MuseApp

final class PlaceholderLobbyCardTableTests: XCTestCase {
    func test_noCards_yieldsNoSpots() {
        XCTAssertEqual(PlaceholderLobbyCardTable.build(cardCount: 0).capacity, 0)
    }

    func test_seatsExactlyTheRequestedCount() {
        for count in [1, 2, 4, 5, 9, 16] {
            XCTAssertEqual(PlaceholderLobbyCardTable.build(cardCount: count).capacity, count)
        }
    }

    func test_beyondTheFixtureCeiling_theTableIsShort_notStacked() {
        let table = PlaceholderLobbyCardTable.build(cardCount: 40)

        XCTAssertEqual(table.capacity, PlaceholderLobbyCardTable.maximumCards)

        let cards = (0..<40).map { LobbyRoomCard(roomID: "r\($0)", name: "R", isMarkedPrivate: false) }
        XCTAssertEqual(
            LobbyCardPlacementResolver.resolve(cards: cards, styleID: table.styleID, table: table),
            .unresolvable(.insufficientCardSpots(needed: 40, available: 16))
        )
    }

    func test_everySpotIsDistinct() {
        let spots = PlaceholderLobbyCardTable.build(cardCount: 16).cardSpots

        let positions = Set(spots.map { "\($0.position.x),\($0.position.y),\($0.position.z)" })
        XCTAssertEqual(positions.count, 16, "two cards must never share a spot")
    }

    func test_everySpotIsInsideTheHall() {
        let spots = PlaceholderLobbyCardTable.build(cardCount: 16).cardSpots
        let halfWidth = PlaceholderLobby.width / 2
        let halfDepth = PlaceholderLobby.depth / 2

        for spot in spots {
            XCTAssertLessThan(abs(spot.position.x), halfWidth, "a card outside the hall could never be reached")
            XCTAssertLessThan(abs(spot.position.z), halfDepth)
            XCTAssertGreaterThan(spot.position.y, 0)
            XCTAssertLessThan(spot.position.y, PlaceholderLobby.height)
        }
    }

    func test_firstRowFillsBeforeTheSecond() {
        let spots = PlaceholderLobbyCardTable.build(cardCount: 6).cardSpots

        let firstRowZ = spots[0].position.z
        XCTAssertEqual(spots[1].position.z, firstRowZ, accuracy: 0.001)
        XCTAssertEqual(spots[2].position.z, firstRowZ, accuracy: 0.001)
        XCTAssertEqual(spots[3].position.z, firstRowZ, accuracy: 0.001)
        XCTAssertLessThan(spots[4].position.z, firstRowZ, "the fifth card starts a row further in")
        XCTAssertEqual(spots[4].position.z, spots[5].position.z, accuracy: 0.001)
    }

    func test_spacingMeansStandingAtOneCardFocusesOnlyThatCard() {
        let table = PlaceholderLobbyCardTable.build(cardCount: 4)
        let placements = table.cardSpots.enumerated().map { index, transform in
            ResolvedLobbyCardPlacement(
                card: LobbyRoomCard(roomID: "r\(index)", name: "R\(index)", isMarkedPrivate: false),
                cardIndex: index,
                transform: transform
            )
        }

        for placement in placements {
            let standingAt = SIMD3<Float>(placement.transform.position.x, 0, placement.transform.position.z + 1)
            XCTAssertEqual(
                LobbyCardFocus.focusedCard(viewerPosition: standingAt, placements: placements)?.roomID,
                placement.roomID
            )
        }
    }

    func test_spawnFocusesNothing() {
        let table = PlaceholderLobbyCardTable.build(cardCount: 16)
        let placements = table.cardSpots.enumerated().map { index, transform in
            ResolvedLobbyCardPlacement(
                card: LobbyRoomCard(roomID: "r\(index)", name: "R", isMarkedPrivate: false),
                cardIndex: index,
                transform: transform
            )
        }

        XCTAssertNil(
            LobbyCardFocus.focusedCard(viewerPosition: PlaceholderLobby.spawnPoint.position, placements: placements),
            "arriving must not pre-select a Room — every card is ahead of the entrance"
        )
    }

    func test_providerAnswersForItsOwnIDOnly() async {
        let provider = PlaceholderLobbyCardTable.Provider()

        let fixture = await provider.cardTable(forStyleID: PlaceholderLobbyCardTable.styleID, cardCount: 3)
        let real = await provider.cardTable(forStyleID: "style_modern", cardCount: 3)

        XCTAssertNotNil(fixture)
        XCTAssertNil(real, "a real Museum Style must never be handed fixture coordinates")
    }
}

@MainActor
final class LobbyCardLayerTests: XCTestCase {
    private var anchor: AnchorEntity!
    private var arView: ARView!

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 320, height: 480), cameraMode: .nonAR, automaticallyConfigureSession: false)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
    }

    private func placements(_ count: Int, privateEvery: Int? = nil) -> [ResolvedLobbyCardPlacement] {
        let table = PlaceholderLobbyCardTable.build(cardCount: count)
        return (0..<count).map { index in
            ResolvedLobbyCardPlacement(
                card: LobbyRoomCard(
                    roomID: "r\(index)",
                    name: "Room \(index)",
                    isMarkedPrivate: privateEvery.map { index % $0 == 0 } ?? false
                ),
                cardIndex: index,
                transform: table.cardSpots[index]
            )
        }
    }

    func test_mountsOneEntityPerCard_keyedByRoomID() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)

        layer.apply(placements(4))

        XCTAssertEqual(layer.mountedRoomIDs, ["r0", "r1", "r2", "r3"])
        XCTAssertEqual(layer.root.children.count, 4)
    }

    func test_cardsAreAtTheirAuthoredPositions() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        let placements = self.placements(3)

        layer.apply(placements)

        for placement in placements {
            XCTAssertEqual(layer.position(forRoom: placement.roomID), placement.transform.position)
        }
    }

    func test_noCardHasACollisionComponent() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)

        layer.apply(placements(6))

        for child in layer.root.children {
            XCTAssertNil(child.components[CollisionComponent.self], "a card must never obstruct movement")
            for grandchild in child.children {
                XCTAssertNil(grandchild.components[CollisionComponent.self], "nor may its focus frame")
            }
        }
    }

    func test_focusFrameIsHiddenUntilFocused() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        layer.apply(placements(3))

        XCTAssertNil(layer.focusedRoomID)
        XCTAssertFalse(layer.isFocusFrameVisible(roomID: "r1"))

        layer.setFocused(roomID: "r1")

        XCTAssertEqual(layer.focusedRoomID, "r1")
        XCTAssertTrue(layer.isFocusFrameVisible(roomID: "r1"))
    }

    func test_focusMovesExclusively() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        layer.apply(placements(3))

        layer.setFocused(roomID: "r0")
        layer.setFocused(roomID: "r2")

        XCTAssertFalse(layer.isFocusFrameVisible(roomID: "r0"), "only one card may be highlighted at a time")
        XCTAssertTrue(layer.isFocusFrameVisible(roomID: "r2"))
    }

    func test_clearingFocus_returnsEveryCardToIdle() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        layer.apply(placements(3))
        layer.setFocused(roomID: "r1")

        layer.setFocused(roomID: nil)

        XCTAssertNil(layer.focusedRoomID)
        XCTAssertFalse(layer.isFocusFrameVisible(roomID: "r1"), "`02`: backing away returns the card to idle")
    }

    func test_reapplyingTheSameCards_rebuildsNothing() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        let placements = self.placements(4)
        layer.apply(placements)
        let entities = layer.root.children.map { ObjectIdentifier($0) }

        layer.apply(placements)

        XCTAssertEqual(layer.root.children.map { ObjectIdentifier($0) }, entities, "an unchanged card must not be rebuilt")
    }

    func test_aCardThatDisappears_isRemoved_andCannotStayFocused() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        let all = placements(3)
        layer.apply(all)
        layer.setFocused(roomID: "r2")

        layer.apply(Array(all.prefix(2)))

        XCTAssertEqual(layer.mountedRoomIDs, ["r0", "r1"])
        XCTAssertNil(layer.focusedRoomID, "a card that no longer exists must not remain focused")
    }

    func test_aCardThatMoves_isRepositionedNotRebuilt() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        let original = placements(2)
        layer.apply(original)
        let entityCount = layer.root.children.count

        let moved = [
            ResolvedLobbyCardPlacement(
                card: original[0].card,
                cardIndex: 0,
                transform: SlotTransform(position: SIMD3<Float>(5, 1.5, -3))
            ),
            original[1]
        ]
        layer.apply(moved)

        XCTAssertEqual(layer.position(forRoom: "r0"), SIMD3<Float>(5, 1.5, -3))
        XCTAssertEqual(layer.root.children.count, entityCount)
    }

    func test_privateAndPublicCardsBothRender() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)

        layer.apply(placements(4, privateEvery: 2))

        XCTAssertEqual(layer.mountedRoomIDs.count, 4)
        for child in layer.root.children {
            XCTAssertNotNil((child as? ModelEntity)?.model, "every card must carry signage geometry")
        }
    }

    func test_signageRendersForEveryNameIncludingEmpty() {
        for name in ["Trabzon", "", "   ", String(repeating: "Very long Room name ", count: 12), "🖼️ Emoji Room"] {
            let card = LobbyRoomCard(roomID: "r", name: name, isMarkedPrivate: false)
            XCTAssertNotNil(
                LobbyCardLayer.renderCardImage(card, widthMetres: 2.2, heightMetres: 1.4),
                "signage must render for '\(name)' rather than leaving a blank panel"
            )
        }
    }

    func test_signageRendersWithThePrivateMarking() {
        let card = LobbyRoomCard(roomID: "r", name: "Studio", isMarkedPrivate: true)

        XCTAssertNotNil(LobbyCardLayer.renderCardImage(card, widthMetres: 2.2, heightMetres: 1.4))
    }

    func test_tearDown_leavesNothingMounted() {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        layer.apply(placements(5))
        layer.setFocused(roomID: "r0")

        layer.tearDown()

        XCTAssertTrue(layer.mountedRoomIDs.isEmpty)
        XCTAssertTrue(layer.root.children.isEmpty)
        XCTAssertNil(layer.focusedRoomID)
    }
}

@MainActor
final class LobbyCardInteractionTests: XCTestCase {
    private var arView: ARView!
    private var host: UIView!
    private var anchor: AnchorEntity!
    private var layer: LobbyCardLayer!
    private var entered: [String] = []
    private var refusedCards: [String] = []

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 320, height: 480), cameraMode: .nonAR, automaticallyConfigureSession: false)
        host = UIView(frame: arView.frame)
        host.addSubview(arView)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
        layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        entered = []
        refusedCards = []
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        host = nil
        anchor = nil
        layer = nil
    }

    private func placements(_ count: Int) -> [ResolvedLobbyCardPlacement] {
        let table = PlaceholderLobbyCardTable.build(cardCount: count)
        return (0..<count).map { index in
            ResolvedLobbyCardPlacement(
                card: LobbyRoomCard(roomID: "r\(index)", name: "Room \(index)", isMarkedPrivate: false),
                cardIndex: index,
                transform: table.cardSpots[index]
            )
        }
    }

    private func makeInteraction(_ placements: [ResolvedLobbyCardPlacement]) -> LobbyCardInteraction {
        layer.apply(placements)
        return LobbyCardInteraction(
            gestureHost: host,
            arView: arView,
            layer: layer,
            placements: placements,
            onEnterRoom: { [weak self] in self?.entered.append($0) },
            onDistantCardTapped: { [weak self] in self?.refusedCards.append($0.roomID) }
        )
    }

    func test_recognizerIsAttachedToTheHostView_notTheARView() throws {
        let interaction = makeInteraction(placements(3))

        let recognizer = try XCTUnwrap(interaction.recognizer)
        XCTAssertTrue(host.gestureRecognizers?.contains(recognizer) ?? false)
        XCTAssertFalse(arView.gestureRecognizers?.contains(recognizer) ?? false)
    }

    func test_recognizerDoesNotStealTouchesFromMovement() throws {
        let interaction = makeInteraction(placements(2))

        let recognizer = try XCTUnwrap(interaction.recognizer)
        XCTAssertFalse(recognizer.cancelsTouchesInView, " movement reads raw touches")
    }

    func test_proximityAloneNeverEnters() {
        let placements = self.placements(3)
        let interaction = makeInteraction(placements)

        interaction.updateFocus(viewerPosition: placements[0].transform.position)

        XCTAssertEqual(layer.focusedRoomID, "r0", "standing at a card must focus it")
        XCTAssertTrue(entered.isEmpty, "walking through a card never enters it")
    }

    func test_walkingAcrossSeveralCards_entersNothing() {
        let placements = self.placements(4)
        let interaction = makeInteraction(placements)

        for placement in placements {
            interaction.updateFocus(viewerPosition: placement.transform.position)
        }

        XCTAssertTrue(entered.isEmpty, "crossing the Lobby must not commit to anything")
    }

    func test_tapOnTheFocusedCard_enters() {
        let placements = self.placements(3)
        let interaction = makeInteraction(placements)
        interaction.updateFocus(viewerPosition: placements[1].transform.position)

        interaction.testTap(roomID: "r1")

        XCTAssertEqual(entered, ["r1"])
    }

    func test_tapOnAnUnfocusedCard_doesNotEnter_andExplainsItself() {
        let placements = self.placements(3)
        let interaction = makeInteraction(placements)
        interaction.updateFocus(viewerPosition: placements[0].transform.position)

        interaction.testTap(roomID: "r2")

        XCTAssertTrue(entered.isEmpty, " rejected tap-to-enter from a distance")
        XCTAssertEqual(refusedCards, ["r2"], "the tap must be explained, not silently swallowed")
    }

    func test_tapWithNothingFocused_entersNothing() {
        let interaction = makeInteraction(placements(3))

        interaction.testTap(roomID: "r0")

        XCTAssertTrue(entered.isEmpty)
    }

    func test_backingAwayThenTapping_entersNothing() {
        let placements = self.placements(2)
        let interaction = makeInteraction(placements)

        interaction.updateFocus(viewerPosition: placements[0].transform.position)
        interaction.updateFocus(viewerPosition: SIMD3<Float>(0, 0, PlaceholderLobby.depth / 2 - 1))
        interaction.testTap(roomID: "r0")

        XCTAssertNil(layer.focusedRoomID)
        XCTAssertTrue(entered.isEmpty, "`02`: backing away deselects, and no navigation occurs")
    }

    func test_focusFollowsTheViewer() {
        let placements = self.placements(4)
        let interaction = makeInteraction(placements)

        interaction.updateFocus(viewerPosition: placements[0].transform.position)
        XCTAssertEqual(layer.focusedRoomID, "r0")

        interaction.updateFocus(viewerPosition: placements[3].transform.position)
        XCTAssertEqual(layer.focusedRoomID, "r3")
    }

    func test_updatingPlacements_keepsFocusWorking() {
        let interaction = makeInteraction(placements(4))
        let fewer = Array(placements(4).prefix(2))

        layer.apply(fewer)
        interaction.update(placements: fewer)
        interaction.updateFocus(viewerPosition: fewer[1].transform.position)
        interaction.testTap(roomID: "r1")

        XCTAssertEqual(entered, ["r1"])
    }

    func test_detach_removesTheGestureAndClearsFocus() throws {
        let placements = self.placements(3)
        let interaction = makeInteraction(placements)
        interaction.updateFocus(viewerPosition: placements[0].transform.position)
        let recognizer = try XCTUnwrap(interaction.recognizer)

        interaction.detach()

        XCTAssertFalse(host.gestureRecognizers?.contains(recognizer) ?? false)
        XCTAssertNil(interaction.recognizer)
        XCTAssertNil(layer.focusedRoomID, "no entry affordance may outlive the Lobby")
    }

    func test_afterDetach_aTapEntersNothing() {
        let placements = self.placements(2)
        let interaction = makeInteraction(placements)
        interaction.updateFocus(viewerPosition: placements[0].transform.position)

        interaction.detach()
        interaction.testTap(roomID: "r0")

        XCTAssertTrue(entered.isEmpty)
    }
}

@MainActor
final class LobbyRuntimeTests: XCTestCase {
    func test_runtimeInterfaceProducesTheLobbyController() throws {
        let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime()
        let content = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 3, viewerRole: .owner))

        let controller = runtime.makeLobbyViewController(content: content) { _ in }

        XCTAssertTrue(controller is RealityKitLobbyViewController)
    }

    func test_fixtureIsAlwaysFixtureGeometry() throws {
        let content = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 5, viewerRole: .owner))

        XCTAssertEqual(content.geometry, .verificationFixture)
        XCTAssertEqual(content.styleID, PlaceholderLobbyCardTable.styleID)
    }

    func test_fixtureShowsBothRolesDifferently() throws {
        let asOwner = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 6, viewerRole: .owner))
        let asVisitor = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 6, viewerRole: .visitor))

        XCTAssertEqual(asOwner.cards.count, 6)
        XCTAssertEqual(asOwner.cards.filter(\.isMarkedPrivate).count, 2, "every third fixture Room is Private")
        XCTAssertEqual(asVisitor.cards.count, 4, "the Private Rooms are absent, not locked")
        XCTAssertTrue(asVisitor.cards.allSatisfy { !$0.isMarkedPrivate })
    }

    func test_fixtureRefusesMoreCardsThanTheHallSeats() {
        XCTAssertNil(
            LobbyRenderingVerificationFixture.makeContent(roomCount: 40, viewerRole: .owner),
            "the fixture hall reports a shortfall rather than stacking cards"
        )
    }

    func test_fixtureCanRenderCountsProductionRoutesPast() throws {
        let zero = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 0, viewerRole: .owner))
        let one = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 1, viewerRole: .owner))

        XCTAssertTrue(zero.placements.isEmpty)
        XCTAssertEqual(one.placements.count, 1)
    }

    private func makeController(roomCount: Int, viewerRole: MuseumViewerRole = .owner) throws -> RealityKitLobbyViewController {
        let content = try XCTUnwrap(
            LobbyRenderingVerificationFixture.makeContent(roomCount: roomCount, viewerRole: viewerRole)
        )
        return RealityKitLobbyViewController(content: content) { _ in }
    }

    func test_sceneIsBuiltOnAppearance_notOnViewLoad() throws {
        let controller = try makeController(roomCount: 4)

        controller.loadViewIfNeeded()
        XCTAssertFalse(controller.isSceneLoaded)

        controller.viewWillAppear(false)

        XCTAssertTrue(controller.isSceneLoaded)
        XCTAssertEqual(controller.cardLayer?.mountedRoomIDs.count, 4)
        XCTAssertNotNil(controller.cardInteraction)
    }

    func test_sceneIsTornDownWhenNoLongerVisible() throws {
        let controller = try makeController(roomCount: 3)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        controller.viewDidDisappear(false)

        XCTAssertFalse(controller.isSceneLoaded)
        XCTAssertNil(controller.cardLayer, "a Lobby scene must never stay resident off screen")
        XCTAssertNil(controller.cardInteraction)
    }

    func test_spawnIsDeterministicAndFocusesNothing() throws {
        let controller = try makeController(roomCount: 8)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertEqual(controller.movementController.subject.position, PlaceholderLobby.spawnPoint.position)
        XCTAssertNil(controller.focusedRoomID, "arriving must not pre-select a Room")
    }

    func test_hintTellsTheViewerHowToEnter() throws {
        let controller = try makeController(roomCount: 4)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertEqual(controller.focusHintText?.trimmingCharacters(in: .whitespaces), "Walk up to a Room to enter it")
    }

    func test_hintNamesTheFocusedRoom() throws {
        let controller = try makeController(roomCount: 4)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        let target = try XCTUnwrap(controller.content.placements.first)

        controller.cardLayer?.setFocused(roomID: target.roomID)
        controller.renderFocusHint()

        XCTAssertEqual(
            controller.focusHintText?.trimmingCharacters(in: .whitespaces),
            "Tap to enter \(target.card.signageText)"
        )
    }

    func test_emptyLobbyShowsAnExplicitState_notAnEmptyHall() throws {
        let controller = try makeController(roomCount: 0)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertEqual(controller.emptyStateText?.trimmingCharacters(in: .whitespaces), "Your Museum has no Rooms yet.")
        XCTAssertNil(controller.focusHintText, "there is nothing to walk up to")
    }

    func test_emptyLobbyForAVisitorUsesO2sWording() throws {
        let rooms = [Room(id: "r", name: "Hidden", variantID: "v", privacy: .private)]
        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor)
        let content = LobbyRuntimeContent(
            museumID: "m",
            styleID: PlaceholderLobbyCardTable.styleID,
            geometry: .verificationFixture,
            viewerRole: .visitor,
            cardTable: PlaceholderLobbyCardTable.build(cardCount: 0),
            placements: []
        )
        XCTAssertTrue(cards.isEmpty)

        let controller = RealityKitLobbyViewController(content: content) { _ in }
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertEqual(controller.emptyStateText?.trimmingCharacters(in: .whitespaces), "No public Rooms yet.")
    }

    func test_replacingContent_swapsTheCardsWithoutRebuildingTheScene() throws {
        let controller = try makeController(roomCount: 2)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        let replacement = try XCTUnwrap(
            LobbyRenderingVerificationFixture.makeContent(roomCount: 6, viewerRole: .owner)
        )

        controller.replaceContent(replacement)

        XCTAssertTrue(controller.isSceneLoaded)
        XCTAssertEqual(controller.cardLayer?.mountedRoomIDs.count, 6)
    }

    func test_committingACardReportsTheRoomID() throws {
        let content = try XCTUnwrap(LobbyRenderingVerificationFixture.makeContent(roomCount: 3, viewerRole: .owner))
        var entered: [String] = []
        let controller = RealityKitLobbyViewController(content: content) { entered.append($0) }
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        let target = try XCTUnwrap(content.placements.first)

        controller.cardInteraction?.updateFocus(viewerPosition: target.transform.position)
        controller.cardInteraction?.testTap(roomID: target.roomID)

        XCTAssertEqual(entered, [target.roomID], "the runtime reports the choice; navigation stays above the seam")
    }
}
