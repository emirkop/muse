import Combine
import RealityKit
import UIKit

public final class RealityKitLobbyViewController: UIViewController {
    private var arView: ARView?

    public private(set) var content: LobbyRuntimeContent
    private let onEnterRoom: @MainActor (String) -> Void

    private(set) var cardLayer: LobbyCardLayer?
    private(set) var cardInteraction: LobbyCardInteraction?

    private(set) var cameraController: RealityKitCameraController?
    private(set) var movementController = MuseumMovementController()
    private var movementControls: MovementControlsView?
    private var sceneUpdateSubscription: (any Cancellable)?
    private var placeholderBody: Entity?
    private var environment: Entity?
    private var collisionResolver: any MovementCollisionResolving = UnobstructedCollisionResolver()

    private let cameraModeControl = UISegmentedControl(items: ["First Person", "Third Person"])
    private let controlSchemeControl = UISegmentedControl(items: ["Gesture", "Assistive"])
    private let focusHintLabel = UILabel()
    private let emptyStateLabel = UILabel()

    public private(set) var isSceneLoaded = false

    public init(content: LobbyRuntimeContent, onEnterRoom: @MainActor @escaping (String) -> Void) {
        self.content = content
        self.onEnterRoom = onEnterRoom
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    public override func viewDidLoad() {
        super.viewDidLoad()
        title = "Lobby"
        view.backgroundColor = .systemBackground
        configureCameraModeControl()
        configureMovementControls()
        configureFocusHintLabel()
        configureEmptyStateLabel()
        renderFocusHint()
        renderEmptyState()
    }

    public override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        loadSceneIfNeeded()
    }

    public override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        tearDownScene()
    }

    deinit {
        arView = nil
    }

    // MARK: - Scene

    private func loadSceneIfNeeded() {
        guard arView == nil else { return }

        let arView = ARView(frame: view.bounds, cameraMode: .nonAR, automaticallyConfigureSession: false)
        arView.translatesAutoresizingMaskIntoConstraints = false
        view.insertSubview(arView, at: 0)

        NSLayoutConstraint.activate([
            arView.topAnchor.constraint(equalTo: view.topAnchor),
            arView.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            arView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            arView.trailingAnchor.constraint(equalTo: view.trailingAnchor)
        ])

        // RealityKit ignores every light in a USD file, so without this the
        // scene has no light source at all and every authored Room renders far
        // darker than it was authored. See SceneEnvironmentLighting.
        SceneEnvironmentLighting.apply(to: arView)

        let anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)

        let camera = RealityKitCameraController()
        camera.attach(to: anchor, in: arView.scene)
        cameraController = camera

        let environment = Self.buildEnvironment(for: content.geometry)
        anchor.addChild(environment)
        self.environment = environment
        collisionResolver = RealityKitCollisionResolver(scene: arView.scene)

        addPlaceholderBody(to: anchor)

        movementController.teleport(to: Self.spawnPoint(for: content.geometry))

        sceneUpdateSubscription = arView.scene.subscribe(to: SceneEvents.Update.self) { [weak self] event in
            self?.advanceMovement(deltaTime: Float(event.deltaTime))
        }

        self.arView = arView
        isSceneLoaded = true
        renderCameraModeControl()
        renderControlSchemeControl()
        syncSubjectToCamera()

        mountCards(under: anchor, in: arView)
        renderFocusHint()
        renderEmptyState()
    }

    private static func buildEnvironment(for geometry: LobbyRuntimeContent.Geometry) -> Entity {
        switch geometry {
        case .verificationFixture:
            return PlaceholderLobby.build()
        }
    }

    private static func spawnPoint(for geometry: LobbyRuntimeContent.Geometry) -> MuseumCameraSubject {
        switch geometry {
        case .verificationFixture:
            return PlaceholderLobby.spawnPoint
        }
    }

    private func addPlaceholderBody(to anchor: AnchorEntity) {
        let body = ModelEntity(
            mesh: .generateBox(size: SIMD3<Float>(0.4, 1.6, 0.25), cornerRadius: 0.12),
            materials: [SimpleMaterial(color: .systemTeal, isMetallic: false)]
        )
        anchor.addChild(body)
        placeholderBody = body
    }

    private func tearDownScene() {
        guard let arView else { return }

        cardInteraction?.detach()
        cardInteraction = nil
        cardLayer?.tearDown()
        cardLayer = nil
        environment = nil
        sceneUpdateSubscription?.cancel()
        sceneUpdateSubscription = nil
        placeholderBody = nil
        collisionResolver = UnobstructedCollisionResolver()
        cameraController?.detach()
        cameraController = nil
        arView.scene.anchors.removeAll()
        arView.removeFromSuperview()
        self.arView = nil
        isSceneLoaded = false
    }

    // MARK: - Cards

    private func mountCards(under anchor: AnchorEntity, in arView: ARView) {
        let layer = LobbyCardLayer()
        anchor.addChild(layer.root)
        layer.apply(content.placements)
        cardLayer = layer

        cardInteraction = LobbyCardInteraction(
            gestureHost: view,
            arView: arView,
            layer: layer,
            placements: content.placements,
            onEnterRoom: { [weak self] roomID in self?.handleEnter(roomID: roomID) },
            onDistantCardTapped: { [weak self] card in self?.handleDistantTap(card) }
        )
    }

    public func replaceContent(_ newContent: LobbyRuntimeContent) {
        content = newContent
        cardLayer?.apply(newContent.placements)
        cardInteraction?.update(placements: newContent.placements)
        renderFocusHint()
        renderEmptyState()
    }

    private func handleEnter(roomID: String) {
        onEnterRoom(roomID)
    }

    private func handleDistantTap(_ card: LobbyRoomCard) {
        focusHintLabel.text = "Walk up to \(card.signageText) to enter it."
    }

    // MARK: - Movement

    private func advanceMovement(deltaTime: Float) {
        guard let movementControls else { return }

        var input = movementControls.consumeInput()
        if movementControls.scheme == .assistive {
            input.yawDelta = movementControls.assistiveTurnRate
                * movementController.configuration.turnSpeed
                * deltaTime
        }

        let positionBeforeMove = movementController.subject.position
        movementController.update(input: input, deltaTime: deltaTime)
        let permitted = collisionResolver.resolve(
            from: positionBeforeMove,
            to: movementController.subject.position
        )
        movementController.applyResolvedPosition(permitted)

        syncSubjectToCamera()

        let previouslyFocused = cardLayer?.focusedRoomID
        cardInteraction?.updateFocus(viewerPosition: movementController.subject.position)
        if cardLayer?.focusedRoomID != previouslyFocused {
            renderFocusHint()
        }

        cameraController?.advanceThirdPersonFraming(towards: false, deltaTime: deltaTime)
    }

    private func syncSubjectToCamera() {
        cameraController?.subject = movementController.subject
        placeholderBody?.transform = Transform(
            scale: .one,
            rotation: movementController.subject.orientation,
            translation: movementController.subject.position + SIMD3<Float>(0, 0.8, 0)
        )
    }

    // MARK: - Chrome

    private func configureCameraModeControl() {
        cameraModeControl.addTarget(self, action: #selector(handleCameraModeChanged), for: .valueChanged)
        cameraModeControl.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(cameraModeControl)

        NSLayoutConstraint.activate([
            cameraModeControl.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            cameraModeControl.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor, constant: -24)
        ])
    }

    private func renderCameraModeControl() {
        guard let cameraController else { return }
        cameraModeControl.selectedSegmentIndex = cameraController.mode == .firstPerson ? 0 : 1
    }

    @objc private func handleCameraModeChanged() {
        cameraController?.setMode(cameraModeControl.selectedSegmentIndex == 0 ? .firstPerson : .thirdPerson)
    }

    private func configureMovementControls() {
        let controls = MovementControlsView(lookSensitivity: movementController.configuration.lookSensitivity)
        controls.translatesAutoresizingMaskIntoConstraints = false
        view.insertSubview(controls, belowSubview: cameraModeControl)
        movementControls = controls

        NSLayoutConstraint.activate([
            controls.topAnchor.constraint(equalTo: view.topAnchor),
            controls.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            controls.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            controls.trailingAnchor.constraint(equalTo: view.trailingAnchor)
        ])

        controlSchemeControl.addTarget(self, action: #selector(handleControlSchemeChanged), for: .valueChanged)
        controlSchemeControl.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(controlSchemeControl)

        NSLayoutConstraint.activate([
            controlSchemeControl.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            controlSchemeControl.bottomAnchor.constraint(equalTo: cameraModeControl.topAnchor, constant: -12)
        ])

        if UIAccessibility.isVoiceOverRunning || UIAccessibility.isSwitchControlRunning || UIAccessibility.isAssistiveTouchRunning {
            setControlScheme(.assistive)
        } else {
            setControlScheme(.gesture)
        }
    }

    private func renderControlSchemeControl() {
        controlSchemeControl.selectedSegmentIndex = movementControls?.scheme == .assistive ? 1 : 0
    }

    private func setControlScheme(_ scheme: MovementControlScheme) {
        movementControls?.scheme = scheme
        renderControlSchemeControl()
    }

    @objc private func handleControlSchemeChanged() {
        setControlScheme(controlSchemeControl.selectedSegmentIndex == 0 ? .gesture : .assistive)
    }

    private func configureFocusHintLabel() {
        focusHintLabel.font = .museScaled(ofSize: 14, weight: .medium)
        focusHintLabel.adjustsFontForContentSizeCategory = true
        focusHintLabel.textColor = .white
        focusHintLabel.textAlignment = .center
        focusHintLabel.numberOfLines = 0
        focusHintLabel.backgroundColor = UIColor(white: 0, alpha: 1)
        focusHintLabel.layer.cornerRadius = 8
        focusHintLabel.layer.masksToBounds = true
        focusHintLabel.translatesAutoresizingMaskIntoConstraints = false
        focusHintLabel.isUserInteractionEnabled = false
        view.addSubview(focusHintLabel)

        NSLayoutConstraint.activate([
            focusHintLabel.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            focusHintLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 12),
            focusHintLabel.widthAnchor.constraint(lessThanOrEqualTo: view.widthAnchor, multiplier: 0.9)
        ])
    }

    private func configureEmptyStateLabel() {
        emptyStateLabel.font = .museScaled(ofSize: 17, weight: .semibold)
        emptyStateLabel.adjustsFontForContentSizeCategory = true
        emptyStateLabel.textColor = .white
        emptyStateLabel.textAlignment = .center
        emptyStateLabel.numberOfLines = 0
        emptyStateLabel.backgroundColor = UIColor(white: 0, alpha: 1)
        emptyStateLabel.layer.cornerRadius = 10
        emptyStateLabel.layer.masksToBounds = true
        emptyStateLabel.isUserInteractionEnabled = false
        emptyStateLabel.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(emptyStateLabel)

        NSLayoutConstraint.activate([
            emptyStateLabel.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            emptyStateLabel.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            emptyStateLabel.widthAnchor.constraint(lessThanOrEqualTo: view.widthAnchor, multiplier: 0.8)
        ])
    }

    // MARK: - Read-outs

    var focusedRoomID: String? { cardLayer?.focusedRoomID }
    var focusHintText: String? { focusHintLabel.isHidden ? nil : focusHintLabel.text }
    var emptyStateText: String? { emptyStateLabel.isHidden ? nil : emptyStateLabel.text }

    func renderFocusHint() {
        guard !content.placements.isEmpty else {
            focusHintLabel.isHidden = true
            return
        }
        focusHintLabel.isHidden = false
        if let focused = content.placements.first(where: { $0.roomID == cardLayer?.focusedRoomID }) {
            focusHintLabel.text = "  Tap to enter \(focused.card.signageText)  "
        } else {
            focusHintLabel.text = "  Walk up to a Room to enter it  "
        }
    }

    private func renderEmptyState() {
        guard content.placements.isEmpty else {
            emptyStateLabel.isHidden = true
            return
        }
        emptyStateLabel.isHidden = false
        emptyStateLabel.text = content.viewerRole == .owner
            ? "  Your Museum has no Rooms yet.  "
            : "  No public Rooms yet.  "
    }
}
