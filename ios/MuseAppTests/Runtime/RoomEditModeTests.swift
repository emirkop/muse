import UIKit
import XCTest
@testable import MuseApp

final class RoomEditModeStateTests: XCTestCase {

    func test_owner_canEnterAndExit() {
        var state = RoomEditModeState(role: .owner)
        XCTAssertTrue(state.canEdit)
        XCTAssertFalse(state.isEditing, "never starts in Edit Mode — entry is explicit")

        XCTAssertTrue(state.enter())
        XCTAssertTrue(state.isEditing)

        state.exit()
        XCTAssertFalse(state.isEditing)
    }

    func test_visitor_cannotEdit_andEnterIsANoOp() {
        var state = RoomEditModeState(role: .visitor)
        XCTAssertFalse(state.canEdit)

        XCTAssertFalse(state.enter())
        XCTAssertFalse(state.isEditing)

        state.toggle()
        XCTAssertFalse(state.isEditing)
    }

    func test_toggle_alternatesForOwner() {
        var state = RoomEditModeState(role: .owner)
        state.toggle()
        XCTAssertTrue(state.isEditing)
        state.toggle()
        XCTAssertFalse(state.isEditing)
    }
}

@MainActor
final class RoomEditModeShellTests: XCTestCase {

    private func controller(role: RoomViewerRole, photoCount: Int = 2) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: role, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService
        )
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        return controller
    }

    private func editToggle(in controller: UIViewController) -> UIBarButtonItem? {
        controller.navigationItem.rightBarButtonItem.flatMap { $0.accessibilityIdentifier == "room-edit-mode-toggle" ? $0 : nil }
    }

    private func banner(in controller: UIViewController) -> UIView? {
        controller.view.subviews.first { $0.accessibilityIdentifier == "room-edit-mode-banner" }
    }

    // MARK: - Owner

    func test_owner_seesTheEditToggle_andEntersAndExitsExplicitly() {
        let controller = controller(role: .owner)
        controller.viewWillAppear(false)

        let toggle = editToggle(in: controller)
        XCTAssertNotNil(toggle, "the owner's Edit control must exist")
        XCTAssertEqual(toggle?.title, "Edit")
        XCTAssertEqual(controller.editMode?.isEditing, false, "entry is explicit — never automatic")
        XCTAssertEqual(banner(in: controller)?.isHidden, true)

        controller.enterEditMode()
        XCTAssertEqual(controller.editMode?.isEditing, true)
        XCTAssertEqual(toggle?.title, "Done", "`02`: an explicit Done action to leave")
        XCTAssertEqual(banner(in: controller)?.isHidden, false, "the mode must be visibly distinct")

        controller.exitEditMode()
        XCTAssertEqual(controller.editMode?.isEditing, false)
        XCTAssertEqual(toggle?.title, "Edit")
        XCTAssertEqual(banner(in: controller)?.isHidden, true)
        controller.viewDidDisappear(false)
    }

    func test_toggleAction_flipsTheMode() {
        let controller = controller(role: .owner)
        controller.viewWillAppear(false)
        let toggle = editToggle(in: controller)!

        _ = toggle.target?.perform(toggle.action, with: toggle)
        XCTAssertEqual(controller.editMode?.isEditing, true)
        _ = toggle.target?.perform(toggle.action, with: toggle)
        XCTAssertEqual(controller.editMode?.isEditing, false)
        controller.viewDidDisappear(false)
    }

    // MARK: - Visitor: structurally absent

    func test_visitor_hasNoEditControl_noState_andCannotEnter() {
        let controller = controller(role: .visitor)
        controller.viewWillAppear(false)

        XCTAssertNil(editToggle(in: controller), "no Edit control may exist for a visitor")
        XCTAssertNil(controller.editMode, "no Edit Mode state may exist for a visitor")

        controller.enterEditMode()
        XCTAssertNil(controller.editMode)
        XCTAssertEqual(banner(in: controller)?.isHidden, true)
        XCTAssertEqual(controller.photoLayer?.mountedSlotIndices.count, 2)
        controller.viewDidDisappear(false)
    }

    func test_skeleton_hasNoEditMode() {
        let controller = RealityKitSceneViewController(content: nil)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertNil(controller.editMode)
        XCTAssertNil(editToggle(in: controller))
        controller.viewDidDisappear(false)
    }

    // MARK: - Room-scoped

    func test_leavingTheRoom_exitsEditMode_andReentryStartsNotEditing() {
        let controller = controller(role: .owner)
        controller.viewWillAppear(false)
        controller.enterEditMode()
        XCTAssertEqual(controller.editMode?.isEditing, true)

        controller.viewDidDisappear(false)
        XCTAssertNil(controller.editMode, "Edit Mode dies with the Room's scene")
        XCTAssertNil(editToggle(in: controller))

        controller.viewWillAppear(false)
        XCTAssertEqual(controller.editMode?.isEditing, false, "re-entry must not resume editing")
        XCTAssertNotNil(editToggle(in: controller))
        controller.viewDidDisappear(false)
    }

    func test_replacingContent_resetsEditMode() {
        let controller = controller(role: .owner)
        controller.viewWillAppear(false)
        controller.enterEditMode()

        controller.replaceContent(RoomRenderingVerificationFixture.makeContent(photoCount: 3))

        XCTAssertEqual(controller.editMode?.isEditing, false)
        controller.viewDidDisappear(false)
    }

    // MARK: - unaffected

    func test_editMode_doesNotDisturbPhotoRendering() async {
        let controller = controller(role: .owner, photoCount: 4)
        controller.viewWillAppear(false)
        controller.enterEditMode()

        let deadline = Date().addingTimeInterval(20)
        while controller.texturedPhotoCount < 4, Date() < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }

        XCTAssertEqual(controller.texturedPhotoCount, 4, "textures load regardless of Edit Mode")
        XCTAssertEqual(controller.photoLayer?.mountedSlotIndices.count, 4)
        controller.exitEditMode()
        XCTAssertEqual(controller.photoLayer?.texturedSlotIndices.count, 4, "leaving Edit Mode changes no photograph")
        controller.viewDidDisappear(false)
    }
}
