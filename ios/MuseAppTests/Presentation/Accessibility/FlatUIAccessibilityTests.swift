import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class FlatUIAccessibilityTests: XCTestCase {

    // MARK: - Dynamic Type

    func test_museScaledFont_matchesTheFixedSizeAtDefault_andGrowsAtAccessibilitySizes() {
        let base = UIFont.systemFont(ofSize: 17, weight: .semibold)
        let metrics = UIFontMetrics(forTextStyle: .body)

        let atDefault = metrics.scaledFont(
            for: base, compatibleWith: UITraitCollection(preferredContentSizeCategory: .large)
        )
        XCTAssertEqual(atDefault.pointSize, base.pointSize, accuracy: 0.01,
                       "A scaled font must render at its authored size at the default content size — which is why the conversion could be applied to thirty screens without changing any of them.")

        let atAX5 = metrics.scaledFont(
            for: base,
            compatibleWith: UITraitCollection(preferredContentSizeCategory: .accessibilityExtraExtraExtraLarge)
        )
        XCTAssertGreaterThan(atAX5.pointSize, base.pointSize + 10,
                             "At AX5 the font must be substantially larger, not fixed.")

        XCTAssertEqual(UIFont.museScaled(ofSize: 17, weight: .semibold).pointSize, 17, accuracy: 0.01)
    }

    func test_museScaledFont_followsTheRampImpliedByItsSize() {
        let ax = UITraitCollection(preferredContentSizeCategory: .accessibilityExtraExtraExtraLarge)

        let captionGrowth = UIFontMetrics(forTextStyle: .caption1)
            .scaledFont(for: .systemFont(ofSize: 12), compatibleWith: ax).pointSize / 12
        let titleGrowth = UIFontMetrics(forTextStyle: .largeTitle)
            .scaledFont(for: .systemFont(ofSize: 34), compatibleWith: ax).pointSize / 34

        XCTAssertGreaterThan(captionGrowth, titleGrowth,
                             "Small text must scale by a larger factor than large text — which is why `museScaled` picks the style nearest a font's size rather than using one style for everything.")
    }

    func test_aScreensLabels_growWhenTheContentSizeCategoryGrows() {
        let screen = AccountCreationViewController(onContinue: {})
        let window = Self.host(screen)
        defer { window.isHidden = true }

        let labels = screen.view.museDescendants(ofType: UILabel.self)
            .filter { !$0.museIsInsideAControl }
        XCTAssertFalse(labels.isEmpty)
        let before = labels.map(\.font.pointSize)

        screen.traitOverrides.preferredContentSizeCategory = .accessibilityExtraExtraExtraLarge
        window.layoutIfNeeded()

        for (label, sizeBefore) in zip(labels, before) {
            XCTAssertTrue(label.adjustsFontForContentSizeCategory,
                          "Every label on a flat screen must opt into Dynamic Type.")
            XCTAssertGreaterThan(label.font.pointSize, sizeBefore,
                                 "\(label.text ?? "label") did not scale.")
        }
    }

    func test_configurationButtonTitles_scaleWithoutIntervention() {
        var configuration = UIButton.Configuration.filled()
        configuration.title = "Agree & Continue"
        let button = UIButton(configuration: configuration)

        let host = UIViewController()
        host.view.addSubview(button)
        let window = Self.host(host)
        defer { window.isHidden = true }
        let before = button.intrinsicContentSize.height

        host.traitOverrides.preferredContentSizeCategory = .accessibilityExtraExtraExtraLarge
        window.layoutIfNeeded()

        XCTAssertGreaterThan(button.intrinsicContentSize.height, before,
                             "A configuration button grows with Dynamic Type, so any fixed height around it clips its title.")
    }

    func test_noTextBearingView_isPinnedToAnExactHeight() {
        for (name, screen) in Self.cheaplyConstructibleScreens() {
            let window = Self.host(screen)
            defer { window.isHidden = true }

            let clamped = screen.view.museAllConstraints().filter { constraint in
                guard type(of: constraint) == NSLayoutConstraint.self,
                      constraint.identifier == nil,
                      constraint.isActive,
                      constraint.firstAttribute == .height,
                      constraint.relation == .equal,
                      constraint.secondItem == nil else { return false }
                let item = constraint.firstItem
                return item is UIButton || item is UILabel || item is UITextField
            }
            XCTAssertEqual(
                clamped.map { "\(type(of: $0.firstItem!)) at \($0.constant) pt" }, [],
                "\(name) pins a text-bearing view to an exact height; use greaterThanOrEqualToConstant."
            )
        }
    }

    func test_noFlatUISurface_usesANonScalingFixedSizeFont() throws {
        let offenders = try MuseSourceTree.flatUISwiftFiles()
            .filter { $0.name != "MuseAccessibility.swift" }
            .filter { $0.contents.contains(".systemFont(ofSize:") }
            .map(\.name)
        XCTAssertEqual(offenders, [],
                       "systemFont(ofSize:) ignores the user's text size; use UIFont.museScaled(ofSize:).")
    }

    // MARK: - Labels that identify what a control acts on

    func test_roomListRowActions_nameTheRoomTheyActOn() async {
        let service = FakeMuseumService()
        service.roomsResult = .success([
            Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .public),
            Room(id: "r2", name: "Study", variantID: "v2", privacy: .private)
        ])
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")
        let screen = RoomListViewController(
            viewModel: viewModel, onCreateRoom: {}, onSelectRoom: { _ in },
            onAddPhotos: { _ in }, onEnterRoom: { _ in }, onEnterLobby: {},
            onOpenRuntimeSkeleton: {}
        )
        screen.loadViewIfNeeded()
        await viewModel.load()

        let labels = screen.view.museDescendants(ofType: UIButton.self)
            .compactMap(\.accessibilityLabel)

        for name in ["The Long Hall", "Study"] {
            XCTAssertTrue(labels.contains("Enter \(name)"), "Missing 'Enter \(name)' — got \(labels)")
            XCTAssertTrue(labels.contains("Add photos to \(name)"), "Missing 'Add photos to \(name)' — got \(labels)")
        }
    }

    func test_roomListRowActions_meetTheMinimumTapTarget() async {
        let service = FakeMuseumService()
        service.roomsResult = .success([Room(id: "r1", name: "Study", variantID: "v1", privacy: .public)])
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")
        let screen = RoomListViewController(
            viewModel: viewModel, onCreateRoom: {}, onSelectRoom: { _ in },
            onAddPhotos: { _ in }, onEnterRoom: { _ in }, onEnterLobby: {},
            onOpenRuntimeSkeleton: {}
        )
        screen.view.frame = CGRect(x: 0, y: 0, width: 390, height: 844)
        screen.loadViewIfNeeded()
        await viewModel.load()
        screen.view.layoutIfNeeded()

        let expected: Set<String> = ["Enter Study", "Add photos to Study"]
        let actions = screen.view.museDescendants(ofType: UIButton.self)
            .filter { expected.contains($0.accessibilityLabel ?? "") }
        XCTAssertEqual(Set(actions.compactMap(\.accessibilityLabel)), expected,
                       "each row action must carry a per-row label, distinct from the screen-level Enter Museum button and from the room name itself")
        XCTAssertEqual(actions.count, 2)
        for action in actions {
            XCTAssertGreaterThanOrEqual(action.bounds.height, 44,
                                        "\(action.accessibilityLabel ?? "") is \(action.bounds.height) pt tall.")
            XCTAssertGreaterThanOrEqual(action.bounds.width, 44)
        }
    }

    func test_credentialFields_carryTheirOwnLabelNotJustAPlaceholder() {
        let field = CredentialScreenViews.makeField(
            placeholder: "Password", secure: true, contentType: .password
        )
        XCTAssertEqual(field.accessibilityLabel, "Password")

        field.text = "hunter2"
        XCTAssertEqual(field.accessibilityLabel, "Password",
                       "The name must survive the field being filled.")
    }

    func test_roomNameField_isNamedRatherThanDescribedByItsExamplePlaceholder() {
        let screen = RoomCreationViewController(
            viewModel: RoomCreationViewModel(), onNameConfirmed: { _ in }
        )
        screen.loadViewIfNeeded()

        let fields = screen.view.museDescendants(ofType: UITextField.self)
        XCTAssertEqual(fields.count, 1)
        XCTAssertEqual(fields.first?.accessibilityLabel, "Room name")
        XCTAssertEqual(fields.first?.placeholder, RoomNamingRules.placeholderExample,
                       "The example placeholder stays — `02` requires it. It just is not the label.")
    }

    // MARK: - State that was carried by colour or by a glyph alone

    func test_avatarSelection_carriesSelectionAsATraitNotOnlyABorder() {
        let screen = AvatarSelectionViewController(
            viewModel: AvatarSelectionViewModel(profileService: FakeProfileService(), accessToken: "t"),
            currentAvatarID: AvatarCatalog.all[1].id,
            onCompleted: { _ in }
        )
        screen.loadViewIfNeeded()

        let buttons = screen.view.museDescendants(ofType: UIButton.self)
            .filter { label in AvatarCatalog.all.contains { $0.displayName == label.accessibilityLabel } }
        XCTAssertEqual(buttons.count, AvatarCatalog.all.count)

        let selected = buttons.filter { $0.accessibilityTraits.contains(.selected) }
        XCTAssertEqual(selected.count, 1, "Exactly one avatar must read as selected.")
        XCTAssertEqual(selected.first?.accessibilityLabel, AvatarCatalog.all[1].displayName)
    }

    func test_privacySwitches_announceTheStateWordNotJustOnOrOff() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: "s", privacy: .public))
        service.roomsResult = .success([
            Room(id: "r1", name: "Study", variantID: "v1", privacy: .private)
        ])
        let viewModel = PrivacySettingsViewModel(museumService: service, accessToken: "t")
        let screen = PrivacySettingsViewController(viewModel: viewModel)
        screen.loadViewIfNeeded()
        await viewModel.load()

        let switches = screen.view.museDescendants(ofType: UISwitch.self)
        XCTAssertEqual(switches.count, 2)
        for toggle in switches {
            let value = try? XCTUnwrap(toggle.accessibilityValue)
            XCTAssertNotNil(value, "\(toggle.accessibilityLabel ?? "switch") announces only on/off.")
            XCTAssertFalse(["on", "off"].contains((value ?? "").lowercased()))
        }
    }

    func test_assignedMusicTrack_readsAsSelectedRatherThanAsACheckMarkGlyph() async {
        let service = FakeMuseumService()
        service.musicCatalog = [
            MusicTrack(id: "t1", displayName: "Ambient One", attribution: "a", licensing: .devTest, durationSeconds: 60),
            MusicTrack(id: "t2", displayName: "Ambient Two", attribution: "a", licensing: .devTest, durationSeconds: 60)
        ]
        let viewModel = RoomMusicSelectionViewModel(
            assignedTrackID: "t2",
            assignment: MuseumRoomMusicAssignment(museumService: service, accessToken: "t", roomID: "r1"),
            musicCatalog: service,
            accessToken: "t"
        )
        let screen = RoomMusicSelectionViewController(viewModel: viewModel, onChanged: { _ in })
        screen.loadViewIfNeeded()
        await viewModel.load()

        let rows = screen.view.museDescendants(ofType: UIButton.self)
            .filter { ($0.accessibilityIdentifier ?? "").hasPrefix("music-track-") }
        XCTAssertEqual(rows.count, 2)

        let assigned = try? XCTUnwrap(rows.first { $0.accessibilityIdentifier == "music-track-t2" })
        XCTAssertEqual(assigned?.accessibilityLabel, "Ambient Two",
                       "The label must be the track's name, with no glyph in it.")
        XCTAssertTrue(assigned?.accessibilityTraits.contains(.selected) == true)

        let unassigned = rows.first { $0.accessibilityIdentifier == "music-track-t1" }
        XCTAssertFalse(unassigned?.accessibilityTraits.contains(.selected) == true)
    }

    func test_overLengthName_statesTheProblemInWordsNotOnlyInRed() {
        let screen = RoomCreationViewController(
            viewModel: RoomCreationViewModel(), onNameConfirmed: { _ in }
        )
        screen.loadViewIfNeeded()

        func countLabel() -> UILabel? {
            screen.view.museDescendants(ofType: UILabel.self)
                .first { ($0.text ?? "").contains("/\(RoomNamingRules.interimMaximumLength)") }
        }

        func type(_ text: String) {
            let field = screen.view.museDescendants(ofType: UITextField.self)[0]
            field.text = text
            field.sendActions(for: .editingChanged)
            screen.view.layoutIfNeeded()
        }

        type("Study")
        XCTAssertEqual(countLabel()?.text, "5/\(RoomNamingRules.interimMaximumLength)")

        type(String(repeating: "x", count: RoomNamingRules.interimMaximumLength + 5))
        let over = countLabel()
        XCTAssertTrue((over?.text ?? "").contains("too long"),
                      "The over-length state must be readable without seeing the colour: got \(over?.text ?? "nil")")
        XCTAssertTrue((over?.accessibilityLabel ?? "").contains("too long"))
    }

    // MARK: - Elements that were invisible to a reader entirely

    func test_photoThumbnails_areReadableElementsWithLabels() {
        let picked = PickedPhoto(
            id: "p1",
            assetIdentifier: nil,
            loadState: .ready(
                thumbnail: Self.onePixelJPEG(),
                file: NormalizedPhotoFile(
                    fileURL: URL(fileURLWithPath: "/tmp/.jpg"),
                    contentType: "image/jpeg", byteSize: 1,
                    pixelWidth: 1, pixelHeight: 1, sha256Hex: String(repeating: "0", count: 64)
                )
            )
        )
        let viewModel = PhotoSelectionViewModel(
            room: Room(id: "r1", name: "Study", variantID: "v1", privacy: .public),
            uploader: FakeRoomPhotoUploader(),
            accessToken: "t"
        )
        let screen = PhotoSelectionViewController(
            viewModel: viewModel, photoPicker: FakePhotoPicker(photos: [picked]), onDone: {}
        )
        screen.loadViewIfNeeded()
        viewModel.ingest([picked])
        screen.view.layoutIfNeeded()

        let strip = try? XCTUnwrap(screen.view.museDescendants(ofType: UIScrollView.self).first)
        let thumbnails = (strip?.museDescendants(ofType: UIImageView.self) ?? [])
            .filter { $0.image != nil }
        XCTAssertFalse(thumbnails.isEmpty, "Expected at least one thumbnail.")
        for thumbnail in thumbnails {
            XCTAssertTrue(thumbnail.isAccessibilityElement,
                          "A thumbnail that is not an element cannot carry the label it already sets.")
            XCTAssertFalse((thumbnail.accessibilityLabel ?? "").isEmpty)
        }
    }

    func test_launchLoadingScreen_isNotSilentToAReader() {
        let screen = LaunchLoadingViewController()
        screen.loadViewIfNeeded()

        XCTAssertEqual(screen.view.museDescendants(ofType: UIActivityIndicatorView.self).count, 1)
        XCTAssertTrue(screen.view.isAccessibilityElement,
                      "UIActivityIndicatorView refuses isAccessibilityElement, so the loading state must be carried by a view that accepts it.")
        XCTAssertEqual(screen.view.accessibilityLabel, "Opening Muse")
        XCTAssertTrue(screen.view.accessibilityTraits.contains(.updatesFrequently))

        screen.showServerUnreachable(message: "Muse can't reach the server.")
        XCTAssertFalse(screen.view.isAccessibilityElement,
                       "Once there is something to act on, the screen must stop being one opaque element.")
        XCTAssertNil(screen.view.accessibilityLabel)
        let retry = screen.view.museDescendants(ofType: UIButton.self)
            .filter { $0.accessibilityIdentifier == "launch-retry" }
        XCTAssertEqual(retry.count, 1)
        XCTAssertFalse(retry[0].isHidden)
    }

    func test_firstLaunchTagline_isAControlAndNotOnAClock() {
        let screen = FirstLaunchViewController(onContinue: {})
        screen.loadViewIfNeeded()

        let tappable = screen.view.museDescendants(ofType: UILabel.self)
            .first { $0.accessibilityTraits.contains(.button) }
        let tagline = try? XCTUnwrap(tappable)
        XCTAssertTrue(tagline?.isAccessibilityElement == true)
        XCTAssertFalse((tagline?.accessibilityHint ?? "").isEmpty,
                       "A tappable label needs to say what tapping does.")
    }

    // MARK: - A label that described the wrong destination

    func test_previewBackButton_usesTheTitleItsCallerPassed() {
        let screen = PreviewViewController(
            viewModel: PreviewViewModel(
                subject: PreviewSubject(
                    id: "v1",
                    displayName: "The Long Gallery",
                    assetBundle: AssetBundleRef(id: "bundle", version: 1)
                ),
                isCurrentlySelected: false,
                confirmationReassurance: nil,
                assetProvider: UnavailablePreviewAssetProvider()
            ),
            backButtonTitle: "Back to Designs",
            onChoose: { _ in },
            onBack: {}
        )
        screen.loadViewIfNeeded()

        let titles = screen.view.museDescendants(ofType: UIButton.self)
            .compactMap { $0.configuration?.title }
        XCTAssertTrue(titles.contains("Back to Designs"), "Got \(titles)")
        XCTAssertFalse(titles.contains("Back to Styles"),
                       "A Room Variant preview must not offer to go back to Styles.")
    }

    // MARK: - Layout that survives an accessibility text size

    func test_horizontalControlRows_becomeVerticalAtAnAccessibilityTextSize() {
        let screen = MainProductChoiceViewController(
            onSelectMuseum: {}, onSelectCollectionRooms: {}, onViewProfile: {}
        )
        let window = Self.host(screen)
        defer { window.isHidden = true }

        func tileRow() -> UIStackView? {
            screen.view.museDescendants(ofType: UIStackView.self)
                .first { row in row.arrangedSubviews.count == 2 && row.arrangedSubviews.allSatisfy { $0 is UIButton } }
        }

        XCTAssertEqual(tileRow()?.axis, .horizontal, "At the default size the tiles sit side by side.")

        screen.traitOverrides.preferredContentSizeCategory = .accessibilityExtraExtraExtraLarge
        window.layoutIfNeeded()

        XCTAssertEqual(tileRow()?.axis, .vertical,
                       "At AX5 two tiles cannot share a phone line without truncating.")
    }

    static func cheaplyConstructibleScreens() -> [(String, UIViewController)] {
        [
            ("AccountCreationViewController", AccountCreationViewController(onContinue: {})),
            ("FirstLaunchViewController", FirstLaunchViewController(onContinue: {})),
            ("LaunchLoadingViewController", LaunchLoadingViewController()),
            ("MainProductChoiceViewController", MainProductChoiceViewController(
                onSelectMuseum: {}, onSelectCollectionRooms: {}, onViewProfile: {}
            )),
            ("MuseumCreationFramingViewController", MuseumCreationFramingViewController(onContinue: {})),
            ("PlaceholderDestinationViewController", PlaceholderDestinationViewController(
                message: "Placeholder", actionTitle: "Open", onAction: {}
            )),
            ("RoomCreationViewController", RoomCreationViewController(
                viewModel: RoomCreationViewModel(), onNameConfirmed: { _ in }
            ))
        ]
    }

    static func host(_ controller: UIViewController) -> UIWindow {
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 390, height: 844))
        window.rootViewController = controller
        window.isHidden = false
        window.layoutIfNeeded()
        return window
    }

    private static func onePixelJPEG() -> Data {
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: 1, height: 1))
        let image = renderer.image { context in
            UIColor.systemTeal.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 1, height: 1))
        }
        return image.jpegData(compressionQuality: 1) ?? Data()
    }
}

// MARK: - Deferred: the 3D exploration surfaces

@MainActor
final class MuseumRuntimeAccessibilityDeferralTests: XCTestCase {

    func test_assistiveMovementControls_areLabelledButRemainHoldOnly() {
        let controls = MovementControlsView(lookSensitivity: 1)
        controls.frame = CGRect(x: 0, y: 0, width: 390, height: 844)
        controls.scheme = .assistive
        controls.layoutIfNeeded()

        let buttons = controls.museDescendants(ofType: UIButton.self)
        XCTAssertEqual(buttons.count, 4, "Turn left, backward, forward, turn right.")
        for button in buttons {
            XCTAssertFalse((button.accessibilityLabel ?? "").isEmpty,
                           " labelled these, and that stands.")
            XCTAssertGreaterThanOrEqual(button.bounds.height, 44)
            XCTAssertTrue(button.accessibilityCustomActions?.isEmpty ?? true,
                          "If this ever gains a custom action, the deferred 3D navigation decision has been taken — record it in `08` first.")
        }
    }

    func test_theRoomSceneSurface_publishesNoAccessibilityElementsForItsContents() {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 3)
        let controller = RealityKitSceneViewController(content: fixture)
        controller.loadViewIfNeeded()

        XCTAssertFalse(controller.view.isAccessibilityElement,
                       "The scene host is not itself a single element…")
        XCTAssertEqual(controller.view.accessibilityElements?.count ?? 0, 0,
                       "…and it publishes no elements for the photographs, captions or sculptures inside it. DEFERRED, not solved.")
    }
}

// MARK: - Test support

extension UIView {
    var museIsInsideAControl: Bool {
        var parent = superview
        while let current = parent {
            if current is UIControl { return true }
            parent = current.superview
        }
        return false
    }

    func museAllConstraints() -> [NSLayoutConstraint] {
        var found = constraints
        for subview in subviews {
            found.append(contentsOf: subview.museAllConstraints())
        }
        return found
    }

    func museDescendants<T: UIView>(ofType type: T.Type) -> [T] {
        var found: [T] = []
        if let match = self as? T { found.append(match) }
        for subview in subviews {
            found.append(contentsOf: subview.museDescendants(ofType: type))
        }
        return found
    }
}

enum MuseSourceTree {
    struct SourceFile {
        let name: String
        let contents: String
    }

    static func flatUISwiftFiles() throws -> [SourceFile] {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("MuseApp/Presentation")

        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: root.path, isDirectory: &isDirectory), isDirectory.boolValue else {
            throw XCTSkip("Flat-UI sources not found at \(root.path)")
        }

        guard let walker = FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil) else {
            throw XCTSkip("Could not enumerate \(root.path)")
        }

        var files: [SourceFile] = []
        for case let url as URL in walker where url.pathExtension == "swift" {
            files.append(SourceFile(name: url.lastPathComponent, contents: try String(contentsOf: url, encoding: .utf8)))
        }
        guard !files.isEmpty else {
            throw XCTSkip("No Swift files under \(root.path)")
        }
        return files.sorted { $0.name < $1.name }
    }
}
