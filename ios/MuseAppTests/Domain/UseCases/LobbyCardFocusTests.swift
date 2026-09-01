import simd
import XCTest
@testable import MuseApp

final class LobbyCardFocusTests: XCTestCase {
    private func placement(_ id: String, index: Int, at position: SIMD3<Float>) -> ResolvedLobbyCardPlacement {
        ResolvedLobbyCardPlacement(
            card: LobbyRoomCard(roomID: id, name: id, isMarkedPrivate: false),
            cardIndex: index,
            transform: SlotTransform(position: position)
        )
    }

    func test_noCards_focusesNothing() {
        XCTAssertNil(LobbyCardFocus.focusedCard(viewerPosition: .zero, placements: []))
    }

    func test_standingFarFromEveryCard_focusesNothing() {
        let placements = [placement("a", index: 0, at: SIMD3<Float>(0, 1.5, -20))]

        XCTAssertNil(
            LobbyCardFocus.focusedCard(viewerPosition: SIMD3<Float>(0, 0, 5), placements: placements),
            "focus must not reach across the hall — that would be tap-from-distance by another route"
        )
    }

    func test_standingInFrontOfACard_focusesIt() {
        let placements = [placement("a", index: 0, at: SIMD3<Float>(0, 1.5, -4))]

        let focused = LobbyCardFocus.focusedCard(viewerPosition: SIMD3<Float>(0, 0, -2.5), placements: placements)

        XCTAssertEqual(focused?.roomID, "a")
    }

    func test_focusesTheNearestOfSeveral() {
        let placements = [
            placement("far", index: 0, at: SIMD3<Float>(-2.5, 1.5, 0)),
            placement("near", index: 1, at: SIMD3<Float>(0.4, 1.5, 0)),
            placement("mid", index: 2, at: SIMD3<Float>(1.8, 1.5, 0))
        ]

        let focused = LobbyCardFocus.focusedCard(viewerPosition: .zero, placements: placements)

        XCTAssertEqual(focused?.roomID, "near")
    }

    func test_heightIsIgnored() {
        let placements = [placement("a", index: 0, at: SIMD3<Float>(0, 12, 0))]

        XCTAssertEqual(
            LobbyCardFocus.focusedCard(viewerPosition: .zero, placements: placements)?.roomID, "a"
        )
    }

    func test_exactlyAtTheRadius_isStillFocused() {
        let radius = LobbyCardFocus.interimFocusRadiusMetres
        let placements = [placement("a", index: 0, at: SIMD3<Float>(radius, 1.5, 0))]

        XCTAssertNotNil(LobbyCardFocus.focusedCard(viewerPosition: .zero, placements: placements))
    }

    func test_justBeyondTheRadius_isNotFocused() {
        let radius = LobbyCardFocus.interimFocusRadiusMetres
        let placements = [placement("a", index: 0, at: SIMD3<Float>(radius + 0.01, 1.5, 0))]

        XCTAssertNil(LobbyCardFocus.focusedCard(viewerPosition: .zero, placements: placements))
    }

    func test_equidistantCards_resolveToTheLowerIndexDeterministically() {
        let placements = [
            placement("right", index: 3, at: SIMD3<Float>(1, 1.5, 0)),
            placement("left", index: 1, at: SIMD3<Float>(-1, 1.5, 0))
        ]

        for _ in 0..<10 {
            XCTAssertEqual(
                LobbyCardFocus.focusedCard(viewerPosition: .zero, placements: placements)?.roomID,
                "left"
            )
        }
    }

    func test_walkingPastACard_focusesThenUnfocuses() {
        let placements = [placement("a", index: 0, at: SIMD3<Float>(0, 1.5, 0))]
        let radius = LobbyCardFocus.interimFocusRadiusMetres

        let approach = LobbyCardFocus.focusedCard(
            viewerPosition: SIMD3<Float>(0, 0, radius + 1), placements: placements
        )
        let alongside = LobbyCardFocus.focusedCard(viewerPosition: SIMD3<Float>(0, 0, 0.2), placements: placements)
        let departed = LobbyCardFocus.focusedCard(
            viewerPosition: SIMD3<Float>(0, 0, -(radius + 1)), placements: placements
        )

        XCTAssertNil(approach)
        XCTAssertEqual(alongside?.roomID, "a", "`02`: approaching produces the focus state")
        XCTAssertNil(departed, "`02`: backing away returns the card to idle, with no navigation")
    }
}
