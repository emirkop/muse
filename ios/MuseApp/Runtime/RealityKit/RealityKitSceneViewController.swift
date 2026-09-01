import Combine
import RealityKit
import UIKit

public final class RealityKitSceneViewController: UIViewController {
    // MARK: - Properties

    private var arView: ARView?

    public private(set) var content: RoomRuntimeContent?

    private(set) var photoLayer: RoomPhotoLayer?
    private var photoLoadTask: Task<Void, Never>?

    private(set) var contentCoordinator: RoomContentEditCoordinator?
    private(set) var reorderInteraction: RoomReorderInteraction?
    private let reorderStatusLabel = UILabel()

    private(set) var captionLayer: RoomCaptionLayer?
    private(set) var photoTapInteraction: RoomPhotoTapInteraction?

    private(set) var sculptureLayer: RoomSculptureLayer?
    private(set) var sculptureCoordinator: RoomSculptureEditCoordinator?
    private var sculptureBarButton: UIBarButtonItem?
    private var musicAssignBarButton: UIBarButtonItem?
    public var onAssignMusic: ((RoomRuntimeContent) -> Void)?

    private let photoPicker: any PhotoPicking
    private var editNotice: String?
    private var editNoticeIsError = false
    private var lastAnnouncedNotice: String?
    private var lastAnnouncedEditingState: Bool?

    public private(set) var texturedPhotoCount = 0
    public private(set) var failedPhotoCount = 0
    private let photoProgressLabel = UILabel()

    public private(set) var musicSession: RoomMusicSession?
    private var musicBarButton: UIBarButtonItem?

    public private(set) var editMode: RoomEditModeState?
    private var editModeBarButton: UIBarButtonItem?
    private let editModeBanner = UILabel()
    private var environment: Entity?
    private(set) var activeBundleLease: AssetBundleLease?

    var environmentVisualExtentsForTesting: SIMD3<Float>? {
        environment?.visualBounds(relativeTo: nil).extents
    }

    /// Whether the scene actually carries environment lighting. RealityKit
    /// ignores USD lights, so this being false means the Room is lit by nothing
    /// at all — the exact regression SceneEnvironmentLighting exists to prevent.
    var hasEnvironmentLightingForTesting: Bool {
        arView?.environment.lighting.resource != nil
    }

    private(set) var cameraController: RealityKitCameraController?

    private(set) var movementController = MuseumMovementController()

    private var movementControls: MovementControlsView?
    private var sceneUpdateSubscription: (any Cancellable)?
    private var placeholderBody: Entity?
    private let diagnostics: any ErrorReporting

    private var collisionResolver: any MovementCollisionResolving = UnobstructedCollisionResolver()

    private let cameraModeControl = UISegmentedControl(items: ["First Person", "Third Person"])
    private let controlSchemeControl = UISegmentedControl(items: ["Gesture", "Assistive"])

    public private(set) var isSceneLoaded = false

    // MARK: - Initialization

    public init(
        content: RoomRuntimeContent?,
        photoPicker: any PhotoPicking = SystemPhotoPicker(),
        diagnostics: any ErrorReporting = NoErrorReporting()
    ) {
        self.diagnostics = diagnostics
        self.content = content
        self.photoPicker = photoPicker
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    public override func viewDidLoad() {
        super.viewDidLoad()
        title = content == nil ? "3D Runtime" : "Room"
        view.backgroundColor = .systemBackground
        configureCameraModeControl()
        configureMovementControls()
        configurePhotoProgressLabel()
        configureEditModeBanner()
        configureReorderStatusLabel()
        if content == nil {
            configureVerificationMenu()
        }
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

    // MARK: - Scene Build & Mount

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
        camera.isFocusZoomEnabled = !UIAccessibility.isReduceMotionEnabled
        cameraController = camera

        let environment: Entity
        switch content?.geometry {
        case .variantBundle(let variant):
            environment = mountDeliveredEnvironment(variant, under: anchor)
        case .verificationFixture, .none:
            environment = PlaceholderRoom.build()
            anchor.addChild(environment)
            movementController.teleport(to: PlaceholderRoom.spawnPoint)
        }
        self.environment = environment
        collisionResolver = RealityKitCollisionResolver(scene: arView.scene)

        addPlaceholderBody(to: anchor)

        sceneUpdateSubscription = arView.scene.subscribe(to: SceneEvents.Update.self) { [weak self] event in
            self?.advanceMovement(deltaTime: Float(event.deltaTime))
        }

        self.arView = arView
        isSceneLoaded = true
        renderCameraModeControl()
        renderControlSchemeControl()
        syncSubjectToCamera()

        mountRoomContentIfPresent(under: anchor)
    }

    private func mountDeliveredEnvironment(_ variant: RoomVariantGeometry, under anchor: AnchorEntity) -> Entity {
        activeBundleLease = content?.bundleRetention?.retain(variant.identity)

        movementController.teleport(to: variant.entry)

        let environment: Entity
        do {
            environment = try Entity.load(contentsOf: variant.fileURL)
            environment.generateCollisionShapes(recursive: true)
        } catch {
            diagnostics.report(ErrorReport(
                domain: .runtimeScene, reason: .sceneLoadFailed,
                bundle: "\(variant.identity.bundleID)@\(variant.identity.version)"
            ))
            environment = Entity()
        }
        anchor.addChild(environment)
        return environment
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

        unmountRoomContent()
        environment = nil
        if let lease = activeBundleLease {
            content?.bundleRetention?.release(lease)
            activeBundleLease = nil
        }
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
        updatePhotoFocus(deltaTime: deltaTime)
    }

    private func updatePhotoFocus(deltaTime: Float) {
        guard let photoLayer else {
            cameraController?.advanceFocusZoom(towards: false, deltaTime: deltaTime)
            cameraController?.advanceThirdPersonFraming(towards: false, deltaTime: deltaTime)
            return
        }

        let focused: FocusedPhoto? = photoLayer.hasLiftedPhoto ? nil : RoomPhotoFocus.focusedPhoto(
            eyePosition: movementController.subject.eyePosition(
                eyeHeight: cameraController?.configuration.eyeHeight ?? MuseumCameraConfiguration.default.eyeHeight
            ),
            forward: movementController.subject.forward,
            placements: photoLayer.mountedPlacements(),
            currentlyFocused: photoLayer.focusedPhotoAssetID
        )

        photoLayer.setFocused(assetID: focused?.photoAssetID)
        cameraController?.advanceFocusZoom(towards: focused != nil, deltaTime: deltaTime)
        cameraController?.advanceThirdPersonFraming(towards: focused != nil, deltaTime: deltaTime)
    }

    private func syncSubjectToCamera() {
        cameraController?.subject = movementController.subject
        placeholderBody?.transform = Transform(
            scale: .one,
            rotation: movementController.subject.orientation,
            translation: movementController.subject.position + SIMD3<Float>(0, 0.8, 0)
        )
    }

    // MARK: - Test seam

    func testMoveViewer(to subject: MuseumCameraSubject, deltaTime: Float = 1.0 / 60) {
        movementController.teleport(to: subject)
        syncSubjectToCamera()
        updatePhotoFocus(deltaTime: deltaTime)
    }

    func testAdvanceFrames(_ frames: Int, deltaTime: Float = 1.0 / 60) {
        for _ in 0..<frames { updatePhotoFocus(deltaTime: deltaTime) }
    }

    // MARK: - Camera mode control

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

    // MARK: - Movement controls

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
}

// MARK: - Room content

extension RealityKitSceneViewController {
    private func mountRoomContentIfPresent(under anchor: AnchorEntity) {
        guard let content else {
            renderPhotoProgress()
            renderReorderStatus()
            return
        }

        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let generation = layer.mount(content.placements)
        photoLayer = layer
        texturedPhotoCount = 0
        failedPhotoCount = 0
        renderPhotoProgress()

        let captions = RoomCaptionLayer()
        anchor.addChild(captions.root)
        captions.apply(content.placements)
        captionLayer = captions

        installSculptures(content: content, under: anchor)

        installEditMode(for: content.viewerRole)
        installMusic(content: content)
        installReordering(content: content, layer: layer)

        let maxLongEdge = RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: content.placements.count)
        let stream = content.textures.textures(
            for: content.placements,
            roomID: content.roomID,
            accessToken: content.accessToken,
            maxLongEdge: maxLongEdge
        )

        photoLoadTask = Task { [weak self] in
            for await event in stream {
                guard let self, !Task.isCancelled else { return }
                await self.apply(event, generation: generation)
            }
        }
    }

    private func apply(_ event: RoomPhotoTextureEvent, generation: Int) async {
        guard let photoLayer, photoLayer.generation == generation else { return }
        switch event {
        case .dimensions(let slot, let width, let height):
            guard let assetID = photoLayer.loadingAssetID(forSlot: slot) else { return }
            photoLayer.setStoredDimensions(pixelWidth: width, pixelHeight: height, forAsset: assetID, generation: generation)
        case .decoded(let slot, let image):
            guard let assetID = photoLayer.loadingAssetID(forSlot: slot) else { return }
            let wasTextured = photoLayer.isTextured(assetID: assetID)
            await photoLayer.apply(image, forAsset: assetID, generation: generation)
            if photoLayer.generation == generation, !wasTextured, photoLayer.isTextured(assetID: assetID) {
                texturedPhotoCount += 1
            }
        case .failed(let slot, let reason):
            guard let assetID = photoLayer.loadingAssetID(forSlot: slot) else { return }
            photoLayer.markFailed(assetID: assetID, generation: generation)
            if reason != .cancelled { failedPhotoCount += 1 }
        }
        renderPhotoProgress()
    }

    private func unmountRoomContent() {
        photoLoadTask?.cancel()
        photoLoadTask = nil
        removeReordering()
        photoLayer?.tearDown()
        photoLayer?.root.removeFromParent()
        photoLayer = nil
        captionLayer?.tearDown()
        captionLayer?.root.removeFromParent()
        captionLayer = nil
        removeSculptures()
        removeMusic()
        removeEditMode()
    }

    // MARK: - Room music

    private func installMusic(content: RoomRuntimeContent) {
        removeMusic()
        guard content.supportsMusicPlayback,
              let catalog = content.musicCatalog,
              let player = content.musicPlayer else { return }

        let session = RoomMusicSession(
            trackID: content.room.musicTrackID,
            catalog: catalog,
            player: player,
            accessToken: content.accessToken
        )
        session.onStateChange = { [weak self] _ in self?.renderMusicControl() }
        musicSession = session

        let button = UIBarButtonItem(
            image: UIImage(systemName: "speaker.wave.2.fill"),
            style: .plain,
            target: self,
            action: #selector(handleMusicToggle)
        )
        button.accessibilityIdentifier = "room-music-toggle"
        musicBarButton = button
        renderMusicControl()

        Task { [weak self] in
            await session.enterRoom()
            self?.renderMusicControl()
        }
    }

    private func removeMusic() {
        musicSession?.leaveRoom()
        musicSession = nil
        musicBarButton = nil
        renderMusicControl()
    }

    @objc private func handleMusicToggle() {
        musicSession?.toggleMute()
        renderMusicControl()
    }

    private func renderMusicControl() {
        guard let session = musicSession, let button = musicBarButton else {
            navigationItem.leftBarButtonItems = nil
            return
        }
        button.image = UIImage(systemName: session.state.isMutedLocally ? "speaker.slash.fill" : "speaker.wave.2.fill")
        button.accessibilityLabel = session.state.toggleTitle
        navigationItem.leftBarButtonItems = [button]
    }

    // MARK: - Sculptures

    private func installSculptures(content: RoomRuntimeContent, under anchor: AnchorEntity) {
        let layer = RoomSculptureLayer(
            slotTable: content.slotTable,
            models: content.sculptureModels ?? UnavailableSculptureModelProvider()
        )
        anchor.addChild(layer.root)
        sculptureLayer = layer

        if content.supportsSculptureEditing, let roomService = content.roomService {
            let coordinator = RoomSculptureEditCoordinator(
                roomID: content.roomID,
                sculptures: content.room.sculptures,
                service: roomService,
                accessToken: content.accessToken
            )
            coordinator.onSculpturesChanged = { [weak self] sculptures in
                Task { @MainActor [weak self] in
                    guard let self, self.sculptureCoordinator === coordinator else { return }
                    await self.sculptureLayer?.apply(sculptures)
                }
            }
            sculptureCoordinator = coordinator
        }

        let initial = content.room.sculptures
        Task { @MainActor [weak self] in
            guard let self, self.sculptureLayer === layer else { return }
            await layer.apply(initial)
        }
    }

    private func removeSculptures() {
        sculptureCoordinator?.deactivate()
        sculptureCoordinator = nil
        sculptureLayer?.tearDown()
        sculptureLayer?.root.removeFromParent()
        sculptureLayer = nil
    }

    @objc private func handleAssignMusicTapped() {
        guard let content, content.supportsMusicAssignment else { return }
        onAssignMusic?(content)
    }

    @objc private func handleSculpturesTapped() {
        presentSculptureManagement()
    }

    func presentSculptureManagement() {
        guard let coordinator = sculptureCoordinator,
              let catalog = content?.catalogService,
              let accessToken = content?.accessToken,
              presentedViewController == nil else { return }

        let controller = SculptureManagementViewController(
            viewModel: SculptureManagementViewModel(catalog: catalog, accessToken: accessToken),
            coordinator: coordinator,
            onFinished: {}
        )
        present(controller, animated: true)
    }

    // MARK: - Reordering

    private func installReordering(content: RoomRuntimeContent, layer: RoomPhotoLayer) {
        guard content.supportsOwnerEditing,
              let photoService = content.photoService,
              let roomService = content.roomService,
              let arView else {
            renderReorderStatus()
            return
        }

        let coordinator = RoomContentEditCoordinator(
            room: content.room,
            slotTable: content.slotTable,
            placements: content.placements,
            photoService: photoService,
            roomService: roomService,
            accessToken: content.accessToken,
            photoReplacer: content.photoReplacer
        )
        coordinator.onPlacementsChanged = { [weak self] placements in
            self?.photoLayer?.relayout(placements)
            self?.captionLayer?.apply(placements)
        }
        coordinator.onStatusChanged = { [weak self] _ in
            self?.renderReorderStatus()
        }
        coordinator.onPhotoAssetReplaced = { [weak self] previous, replacement in
            self?.photoLayer?.commitReplacement(from: previous, to: replacement)
        }
        coordinator.onPhotoRemovalBegan = { [weak self] assetID in
            self?.photoLayer?.beginRemoval(assetID: assetID)
        }
        coordinator.onPhotoRemovalCommitted = { [weak self] assetID in
            self?.photoLayer?.commitRemoval(assetID: assetID)
        }
        coordinator.onPhotoRemovalReverted = { [weak self] assetID in
            self?.photoLayer?.revertRemoval(assetID: assetID)
        }
        contentCoordinator = coordinator

        let reorder = RoomReorderInteraction(
            gestureHost: view,
            arView: arView,
            layer: layer,
            isEditing: { [weak self] in self?.editMode?.isEditing == true },
            onSwap: { [weak self] from, to in
                self?.contentCoordinator?.swap(from: from, to: to)
            }
        )
        reorderInteraction = reorder

        photoTapInteraction = RoomPhotoTapInteraction(
            gestureHost: view,
            arView: arView,
            layer: layer,
            requiringFailureOf: reorder.recognizer,
            isEditing: { [weak self] in self?.editMode?.isEditing == true },
            onPhotoTapped: { [weak self] assetID in
                self?.presentPhotoActions(forAsset: assetID)
            }
        )
        renderReorderStatus()
    }

    private func removeReordering() {
        reorderInteraction?.detach()
        reorderInteraction = nil
        photoTapInteraction?.detach()
        photoTapInteraction = nil
        contentCoordinator?.deactivate()
        contentCoordinator = nil
        editNotice = nil
        renderReorderStatus()
    }

    // MARK: - The tapped photograph's action set (phases 40–41)

    private func presentPhotoActions(forAsset assetID: String) {
        guard let coordinator = contentCoordinator,
              let caption = coordinator.caption(forAssetID: assetID),
              presentedViewController == nil else { return }

        let sheet = UIAlertController(title: nil, message: nil, preferredStyle: .actionSheet)
        sheet.view.accessibilityIdentifier = "room-photo-actions"
        if coordinator.isReplacementAvailable {
            sheet.addAction(UIAlertAction(title: "Replace Photo…", style: .default) { [weak self] _ in
                DispatchQueue.main.async { self?.beginReplacement(forAsset: assetID) }
            })
        }
        sheet.addAction(UIAlertAction(title: caption.isEmpty ? "Add Caption" : "Edit Caption", style: .default) { [weak self] _ in
            DispatchQueue.main.async { self?.presentCaptionEditor(forAsset: assetID) }
        })
        sheet.addAction(UIAlertAction(title: "Delete Photo", style: .destructive) { [weak self] _ in
            DispatchQueue.main.async { self?.confirmDeletion(forAsset: assetID) }
        })
        sheet.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        anchorPopover(of: sheet)
        present(sheet, animated: true)
    }

    private func anchorPopover(of alert: UIAlertController) {
        alert.popoverPresentationController?.sourceView = view
        alert.popoverPresentationController?.sourceRect = CGRect(x: view.bounds.midX, y: view.bounds.maxY - 1, width: 1, height: 1)
    }

    // MARK: - Deletion

    private func confirmDeletion(forAsset assetID: String) {
        guard let coordinator = contentCoordinator,
              coordinator.caption(forAssetID: assetID) != nil,
              presentedViewController == nil else { return }

        let confirm = UIAlertController(
            title: "Delete this photograph?",
            message: "It comes off the wall and the photographs after it move up to close the gap. You can add it to the Room again later.",
            preferredStyle: .actionSheet
        )
        confirm.view.accessibilityIdentifier = "room-photo-delete-confirm"
        confirm.addAction(UIAlertAction(title: "Delete Photograph", style: .destructive) { [weak self] _ in
            DispatchQueue.main.async { self?.deletePhoto(forAsset: assetID) }
        })
        confirm.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        anchorPopover(of: confirm)
        present(confirm, animated: true)
    }

    func deletePhoto(forAsset assetID: String) {
        guard let coordinator = contentCoordinator else { return }
        setEditNotice(nil)

        Task { [weak self] in
            guard let self else { return }
            let outcome = await coordinator.deletePhoto(assetID: assetID)
            guard self.contentCoordinator === coordinator else { return }
            switch outcome {
            case .deleted:
                self.setEditNotice(nil)
            case .rejected(let message), .failed(let message):
                self.setEditNotice(message, isError: true)
            }
            self.refreshPhotoCounts()
        }
    }

    // MARK: - Replacement

    func beginReplacement(forAsset assetID: String) {
        guard let coordinator = contentCoordinator, coordinator.isReplacementAvailable,
              presentedViewController == nil else { return }
        setEditNotice(nil)

        Task { [weak self] in
            guard let self else { return }
            let picked = await self.photoPicker.pickPhotos(limit: 1, presentingFrom: self)
            guard let photo = picked.first else { return }
            await self.performReplacement(of: assetID, with: photo, using: coordinator)
        }
    }

    private func performReplacement(of assetID: String, with photo: PickedPhoto, using coordinator: RoomContentEditCoordinator) async {
        guard contentCoordinator === coordinator, let photoLayer else { return }
        guard let file = photo.normalizedFile else {
            setEditNotice(RoomContentEditCoordinator.replacementCouldNotLoadMessage, isError: true)
            return
        }

        let maxLongEdge = RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: coordinator.room.photoSlots.count)
        let preview = await Self.decodeReplacementPreview(file, maxLongEdge: maxLongEdge)
        guard contentCoordinator === coordinator, self.photoLayer === photoLayer else { return }

        var isPreviewing = false
        if let preview {
            isPreviewing = await photoLayer.beginReplacementPreview(preview, forAsset: assetID)
            guard contentCoordinator === coordinator, self.photoLayer === photoLayer else { return }
        }
        refreshPhotoCounts()
        setEditNotice(Self.replacingMessage, isError: false)

        let outcome = await coordinator.replacePhoto(assetID: assetID, with: photo)
        guard contentCoordinator === coordinator, self.photoLayer === photoLayer else { return }

        switch outcome {
        case .replaced:
            setEditNotice(nil)
        case .rejected(let message):
            if isPreviewing { photoLayer.revertReplacementPreview(forAsset: assetID) }
            setEditNotice(message, isError: true)
        case .failed(let reason):
            if isPreviewing { photoLayer.revertReplacementPreview(forAsset: assetID) }
            setEditNotice(RoomContentEditCoordinator.replacementFailureMessage(for: reason), isError: true)
        }
        refreshPhotoCounts()
    }

    private static func decodeReplacementPreview(_ file: NormalizedPhotoFile, maxLongEdge: Int) async -> DecodedPhotoImage? {
        await Task.detached(priority: .userInitiated) {
            guard let data = try? Data(contentsOf: file.fileURL, options: .mappedIfSafe) else { return nil }
            return try? PhotoTextureDecoder.decode(data, maxLongEdge: maxLongEdge)
        }.value
    }

    private func refreshPhotoCounts() {
        guard let photoLayer else { return }
        texturedPhotoCount = photoLayer.texturedAssetIDs.count
        failedPhotoCount = photoLayer.failedAssetIDs.count
        renderPhotoProgress()
    }

    private func setEditNotice(_ text: String?, isError: Bool = false) {
        editNotice = text
        editNoticeIsError = isError
        renderReorderStatus()
    }

    static let replacingMessage = "Replacing photograph…"

    var editNoticeForTesting: String? { editNotice }

    // MARK: - Captions

    private func presentCaptionEditor(forAsset assetID: String) {
        guard let coordinator = contentCoordinator,
              let current = coordinator.caption(forAssetID: assetID),
              presentedViewController == nil else { return }

        let editor = CaptionEditorViewController(
            caption: current,
            save: { [weak self] text in
                guard let self, let coordinator = self.contentCoordinator else {
                    return .failed(message: RoomContentEditCoordinator.failedCaptionMessage)
                }
                return await coordinator.setCaption(text, forAssetID: assetID)
            },
            onFinished: {}
        )
        present(editor, animated: true)
    }

    private func configureReorderStatusLabel() {
        reorderStatusLabel.font = .museScaled(ofSize: 12, weight: .medium)
        reorderStatusLabel.adjustsFontForContentSizeCategory = true
        reorderStatusLabel.textColor = .white
        reorderStatusLabel.backgroundColor = .systemRed
        reorderStatusLabel.layer.cornerRadius = 6
        reorderStatusLabel.clipsToBounds = true
        reorderStatusLabel.textAlignment = .center
        reorderStatusLabel.numberOfLines = 0
        reorderStatusLabel.isHidden = true
        reorderStatusLabel.accessibilityIdentifier = "room-reorder-status"
        reorderStatusLabel.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(reorderStatusLabel)
        NSLayoutConstraint.activate([
            reorderStatusLabel.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            reorderStatusLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 38),
            reorderStatusLabel.widthAnchor.constraint(lessThanOrEqualTo: view.widthAnchor, multiplier: 0.8)
        ])
    }

    private func renderReorderStatus() {
        if let message = contentCoordinator?.statusMessage {
            reorderStatusLabel.isHidden = false
            reorderStatusLabel.backgroundColor = .systemRed
            announceNoticeIfChanged(message, isFailure: true)
            reorderStatusLabel.text = "  \(message)  "
            return
        }
        guard let notice = editNotice else {
            reorderStatusLabel.isHidden = true
            lastAnnouncedNotice = nil
            return
        }
        reorderStatusLabel.isHidden = false
        reorderStatusLabel.backgroundColor = editNoticeIsError ? .systemRed : .systemIndigo
        announceNoticeIfChanged(notice, isFailure: editNoticeIsError)
        reorderStatusLabel.text = "  \(notice)  "
    }

    private func announceNoticeIfChanged(_ message: String, isFailure: Bool) {
        guard message != lastAnnouncedNotice else { return }
        lastAnnouncedNotice = message
        if isFailure {
            MuseAccessibility.announceFailure(message)
        } else {
            MuseAccessibility.announce(message)
        }
    }

    public func replaceContent(_ newContent: RoomRuntimeContent?) {
        unmountRoomContent()
        content = newContent
        title = content == nil ? "3D Runtime" : "Room"
        guard let arView, let anchor = arView.scene.anchors.first as? AnchorEntity else { return }
        mountRoomContentIfPresent(under: anchor)
    }

    // MARK: Progress read-out

    private func configurePhotoProgressLabel() {
        photoProgressLabel.font = .monospacedDigitSystemFont(ofSize: 12, weight: .medium)
        photoProgressLabel.textColor = .white
        photoProgressLabel.backgroundColor = UIColor.black.withAlphaComponent(0.45)
        photoProgressLabel.layer.cornerRadius = 6
        photoProgressLabel.clipsToBounds = true
        photoProgressLabel.textAlignment = .center
        photoProgressLabel.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(photoProgressLabel)
        NSLayoutConstraint.activate([
            photoProgressLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 8),
            photoProgressLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 12),
            photoProgressLabel.heightAnchor.constraint(equalToConstant: 22),
            photoProgressLabel.widthAnchor.constraint(greaterThanOrEqualToConstant: 120)
        ])
        renderPhotoProgress()
    }

    private func renderPhotoProgress() {
        guard let content else {
            photoProgressLabel.isHidden = true
            return
        }
        photoProgressLabel.isHidden = false
        let total = contentCoordinator?.room.photoSlots.count ?? content.placements.count
        let edge = RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: total)
        var text = "  photos \(texturedPhotoCount)/\(total) · \(edge)px  "
        if failedPhotoCount > 0 { text += "· \(failedPhotoCount) failed  " }
        photoProgressLabel.text = text
    }

    // MARK: Verification mode (skeleton only)

    private func configureVerificationMenu() {
        let counts = [1, 2, 4, 5, 14, 27, 28]
        var actions: [UIAction] = counts.map { count in
            UIAction(title: "\(count) photo\(count == 1 ? "" : "s")") { [weak self] _ in
                self?.replaceContent(RoomRenderingVerificationFixture.makeContent(photoCount: count))
            }
        }
        actions.append(UIAction(title: "None", attributes: .destructive) { [weak self] _ in
            self?.replaceContent(nil)
        })

        navigationItem.rightBarButtonItem = UIBarButtonItem(
            title: "Verify",
            image: UIImage(systemName: "photo.on.rectangle.angled"),
            primaryAction: nil,
            menu: UIMenu(children: [
                UIMenu(title: "Photo placement fixture (not a Room)", children: actions),
                lobbyVerificationMenu(),
                collectionItemVerificationMenu()
            ])
        )
    }

    private func collectionItemVerificationMenu() -> UIMenu {
        let actions = CollectionItemPlacementFixture.offeredItemCounts.map { count in
            UIAction(title: "\(count) item\(count == 1 ? "" : "s")") { [weak self] _ in
                self?.navigationController?.pushViewController(
                    CollectionItemPlacementFixtureViewController(itemCount: count),
                    animated: true
                )
            }
        }
        return UIMenu(title: "Collection item fixture (not a Collection Room)", children: actions)
    }

    private func lobbyVerificationMenu() -> UIMenu {
        let counts = [0, 1, 2, 5, 8, 16]
        func actions(for role: MuseumViewerRole) -> [UIAction] {
            counts.map { count in
                UIAction(title: "\(count) Room\(count == 1 ? "" : "s")") { [weak self] _ in
                    self?.presentLobbyFixture(roomCount: count, viewerRole: role)
                }
            }
        }
        return UIMenu(title: "Lobby fixture (not a Lobby)", children: [
            UIMenu(title: "As owner", children: actions(for: .owner)),
            UIMenu(title: "As visitor", children: actions(for: .visitor))
        ])
    }

    private func presentLobbyFixture(roomCount: Int, viewerRole: MuseumViewerRole) {
        guard let content = LobbyRenderingVerificationFixture.makeContent(
            roomCount: roomCount,
            viewerRole: viewerRole
        ) else { return }

        let runtime = RealityKitMuseumRuntime()
        let lobby = runtime.makeLobbyViewController(content: content) { [weak navigation = navigationController] roomID in
            let alert = UIAlertController(
                title: "Entered card",
                message: "Fixture card committed: \(roomID)",
                preferredStyle: .alert
            )
            alert.addAction(UIAlertAction(title: "OK", style: .default))
            navigation?.topViewController?.present(alert, animated: true)
        }
        navigationController?.pushViewController(lobby, animated: true)
    }
}

// MARK: - Owner Edit Mode

extension RealityKitSceneViewController {
    private func installEditMode(for role: RoomViewerRole) {
        removeEditMode()
        let state = RoomEditModeState(role: role)
        guard state.canEdit else {
            editMode = nil
            return
        }
        editMode = state

        let button = UIBarButtonItem(title: "Edit", style: .plain, target: self, action: #selector(handleEditModeToggle))
        button.accessibilityIdentifier = "room-edit-mode-toggle"
        editModeBarButton = button

        if content?.supportsSculptureEditing == true {
            let sculptures = UIBarButtonItem(
                image: UIImage(systemName: "cube"),
                style: .plain,
                target: self,
                action: #selector(handleSculpturesTapped)
            )
            sculptures.accessibilityLabel = "Sculptures"
            sculptures.accessibilityIdentifier = "room-sculptures-action"
            sculptureBarButton = sculptures
        }

        if content?.supportsMusicAssignment == true {
            let music = UIBarButtonItem(
                image: UIImage(systemName: "music.note.list"),
                style: .plain,
                target: self,
                action: #selector(handleAssignMusicTapped)
            )
            music.accessibilityLabel = "Room Music"
            music.accessibilityIdentifier = "room-music-assign"
            musicAssignBarButton = music
        }

        navigationItem.rightBarButtonItem = button
        renderEditMode()
    }

    private func removeEditMode() {
        if editMode?.isEditing == true {
            setEnvironmentDimmed(false)
        }
        editMode = nil
        if navigationItem.rightBarButtonItem === editModeBarButton {
            navigationItem.rightBarButtonItems = nil
        }
        editModeBarButton = nil
        sculptureBarButton = nil
        musicAssignBarButton = nil
        editModeBanner.isHidden = true
    }

    public func enterEditMode() {
        guard var state = editMode, state.enter() else { return }
        editMode = state
        renderEditMode()
    }

    public func exitEditMode() {
        guard var state = editMode else { return }
        state.exit()
        editMode = state
        reorderInteraction?.testCancel()
        setEditNotice(nil)
        renderEditMode()
    }

    @objc private func handleEditModeToggle() {
        if editMode?.isEditing == true {
            exitEditMode()
        } else {
            enterEditMode()
        }
    }

    private func renderEditMode() {
        guard let editMode else {
            editModeBanner.isHidden = true
            return
        }
        editModeBarButton?.title = editMode.isEditing ? "Done" : "Edit"
        editModeBarButton?.style = editMode.isEditing ? .done : .plain
        editModeBanner.isHidden = !editMode.isEditing
        setEnvironmentDimmed(editMode.isEditing)
        if editMode.isEditing != lastAnnouncedEditingState {
            lastAnnouncedEditingState = editMode.isEditing
            MuseAccessibility.announce(editMode.isEditing ? "Editing this Room" : "Finished editing")
        }

        var items: [UIBarButtonItem] = []
        if let editModeBarButton { items.append(editModeBarButton) }
        if editMode.isEditing, let sculptureBarButton { items.append(sculptureBarButton) }
        if editMode.isEditing, let musicAssignBarButton { items.append(musicAssignBarButton) }
        navigationItem.rightBarButtonItems = items
    }

    private func configureEditModeBanner() {
        editModeBanner.text = "  Editing  "
        editModeBanner.font = .museScaled(ofSize: 12, weight: .semibold)
        editModeBanner.adjustsFontForContentSizeCategory = true
        editModeBanner.textColor = .white
        editModeBanner.backgroundColor = .systemIndigo
        editModeBanner.layer.cornerRadius = 6
        editModeBanner.clipsToBounds = true
        editModeBanner.isHidden = true
        editModeBanner.accessibilityIdentifier = "room-edit-mode-banner"
        editModeBanner.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(editModeBanner)
        NSLayoutConstraint.activate([
            editModeBanner.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 8),
            editModeBanner.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -12),
            editModeBanner.heightAnchor.constraint(greaterThanOrEqualToConstant: 22)
        ])
    }

    private func setEnvironmentDimmed(_ dimmed: Bool) {
        guard let environment else { return }
        let color: UIColor = dimmed ? UIColor(white: 0.35, alpha: 1) : .systemGray
        for case let slab as ModelEntity in environment.children {
            slab.model?.materials = [SimpleMaterial(color: color, isMetallic: false)]
        }
    }
}
