import RealityKit
import UIKit
import simd

@MainActor
final class CollectionItemPlacementFixtureViewController: UIViewController {

    private let table: CollectionTierTable
    private let store: FixtureCollectionItemStore
    private var coordinator: CollectionItemEditCoordinator?

    private var arView: ARView?
    private var layer: CollectionItemLayer?
    private var drag: CollectionItemDragInteraction?

    private var isRearranging = true

    private let statusLabel = UILabel()
    private let editToggle = UISegmentedControl(items: ["Editing", "View only"])

    init(itemCount: Int) {
        self.table = CollectionItemPlacementFixture.tierTable()
        self.store = FixtureCollectionItemStore(room: CollectionItemPlacementFixture.room(itemCount: itemCount))
        super.init(nibName: nil, bundle: nil)
        title = "Item placement fixture"
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        configureStatusLabel()
        configureEditToggle()
        configureAddButton()
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        loadSceneIfNeeded()
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        coordinator?.deactivate()
        drag?.detach()
        layer?.tearDown()
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

        SceneEnvironmentLighting.apply(to: arView)

        let anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
        anchor.addChild(Self.buildFixtureSpace())

        let camera = PerspectiveCamera()
        camera.camera.fieldOfViewInDegrees = 55
        camera.position = table.entry.position + SIMD3<Float>(0, 1.6, 1.1)
        camera.orientation = simd_quatf(angle: -0.32, axis: SIMD3<Float>(1, 0, 0))
        anchor.addChild(camera)

        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        self.layer = layer

        let coordinator = CollectionItemEditCoordinator(
            room: CollectionItemPlacementFixture.room(itemCount: 0),
            table: table,
            items: store,
            rooms: store,
            accessToken: "fixture"
        )
        coordinator.onPlacementsChanged = { [weak self] placements in
            self?.layer?.apply(placements)
            self?.renderStatus()
        }
        coordinator.onStatusChanged = { [weak self] _ in self?.renderStatus() }
        self.coordinator = coordinator

        Task { [weak self] in
            guard let self,
                  let room = try? await store.fetchCollectionRoom(accessToken: "fixture", collectionRoomID: "")
            else { return }
            coordinator.adopt(room)
        }

        drag = CollectionItemDragInteraction(
            gestureHost: view,
            arView: arView,
            layer: layer,
            isEditing: { [weak self] in self?.isRearranging ?? false },
            availableEmptySlots: { [weak self] in self?.availableEmptySlots() ?? [] },
            onDrop: { [weak self] itemID, slotIndex in
                self?.coordinator?.drop(itemID: itemID, onSlot: slotIndex)
            }
        )

        self.arView = arView
        renderStatus()
    }

    private func availableEmptySlots() -> [CollectionItemSlot] {
        guard let coordinator else { return [] }
        let occupied = Set(coordinator.room.items.map(\.slotIndex))
        return table.availableSlots(atTier: coordinator.room.currentTier)
            .filter { !occupied.contains($0.slotIndex) }
    }

    private static func buildFixtureSpace() -> Entity {
        let container = Entity()

        var floorMaterial = PhysicallyBasedMaterial()
        floorMaterial.baseColor = .init(tint: UIColor(white: 0.16, alpha: 1))
        floorMaterial.roughness = .init(floatLiteral: 0.95)
        let floor = ModelEntity(mesh: .generatePlane(width: 8, depth: 8), materials: [floorMaterial])
        floor.position = SIMD3<Float>(0, 0, -1)
        container.addChild(floor)

        var panelMaterial = PhysicallyBasedMaterial()
        panelMaterial.baseColor = .init(tint: UIColor(white: 0.24, alpha: 1))
        panelMaterial.roughness = .init(floatLiteral: 0.9)
        let panel = ModelEntity(mesh: .generateBox(size: SIMD3<Float>(6, 3.2, 0.1)), materials: [panelMaterial])
        panel.position = SIMD3<Float>(0, 1.6, -4.2)
        container.addChild(panel)

        return container
    }

    // MARK: - Chrome

    private func configureStatusLabel() {
        statusLabel.numberOfLines = 0
        statusLabel.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        statusLabel.textColor = .white
        statusLabel.backgroundColor = UIColor.black.withAlphaComponent(0.55)
        statusLabel.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(statusLabel)
        NSLayoutConstraint.activate([
            statusLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 8),
            statusLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 12),
            statusLabel.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -12)
        ])
    }

    private func configureEditToggle() {
        editToggle.selectedSegmentIndex = 0
        editToggle.addTarget(self, action: #selector(handleEditToggle), for: .valueChanged)
        editToggle.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(editToggle)
        NSLayoutConstraint.activate([
            editToggle.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            editToggle.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor, constant: -20)
        ])
    }

    private func configureAddButton() {
        navigationItem.rightBarButtonItems = [
            UIBarButtonItem(
                title: "Add",
                primaryAction: UIAction { [weak self] _ in self?.addItem() }
            ),
            UIBarButtonItem(
                title: "Fail next",
                primaryAction: UIAction { [weak self] _ in
                    guard let self else { return }
                    Task {
                        await self.store.failNextWrite()
                        self.renderStatus()
                    }
                }
            )
        ]
    }

    @objc private func handleEditToggle() {
        isRearranging = editToggle.selectedSegmentIndex == 0
        renderStatus()
    }

    private func addItem() {
        guard let coordinator else { return }
        let addition = CollectionItemAddition(
            expansion: CollectionTierExpansion(
                ratchet: FixtureTierRatchet(store: store),
                geometry: FixtureTierGeometry()
            ),
            items: store
        )
        Task { [weak self] in
            let result = await addition.add(
                catalogModelID: "dev-fixture:model-chrono-one",
                to: coordinator.room,
                table: self?.table ?? CollectionItemPlacementFixture.tierTable(),
                accessToken: "fixture"
            )
            guard let self else { return }
            switch result {
            case .success(let outcome):
                coordinator.adopt(outcome.room)
                self.lastAdditionNote = "added at slot \(outcome.placedItem.slotIndex)"
            case .failure(let failure):
                self.lastAdditionNote = "add refused: \(failure)"
            }
            self.renderStatus()
        }
    }

    private var lastAdditionNote = ""

    private func renderStatus() {
        guard let coordinator else { return }
        let occupied = coordinator.room.items
            .sorted { $0.slotIndex < $1.slotIndex }
            .map { String($0.slotIndex) }
            .joined(separator: ",")
        let available = availableEmptySlots().map { String($0.slotIndex) }.joined(separator: ",")
        var lines = [
            " DEV FIXTURE — not a Collection Room. Slots and capacities are engineering values. ",
            "  tier \(coordinator.room.currentTier.ordinal)/\(CollectionItemPlacementFixture.cumulativeCapacities.count)"
                + " · items \(coordinator.room.itemCount) at [\(occupied)]  ",
            "  free reached slots [\(available)] · \(isRearranging ? "long-press an item to rearrange" : "view only")  ",
            "  status \(coordinator.status)  "
        ]
        if !coordinator.unresolvableItems.isEmpty {
            lines.append("  \(coordinator.unresolvableItems.count) item(s) on slots this design does not author  ")
        }
        if !lastAdditionNote.isEmpty {
            lines.append("  \(lastAdditionNote)  ")
        }
        statusLabel.text = lines.joined(separator: "\n")
    }
}

private final class FixtureTierRatchet: CollectionTierRatcheting, @unchecked Sendable {
    private let store: FixtureCollectionItemStore
    init(store: FixtureCollectionItemStore) { self.store = store }

    func ratchetTier(
        accessToken: String, collectionRoomID: String, tier: CollectionTier
    ) async throws -> CollectionRoom {
        try await store.ratchetFixtureTier(to: tier)
    }
}

private struct FixtureTierGeometry: CollectionTierGeometryInstalling {
    func installTierGeometry(accessToken: String, bundle: AssetBundleRef) async -> Bool { true }
}

extension FixtureCollectionItemStore {
    func ratchetFixtureTier(to tier: CollectionTier) async throws -> CollectionRoom {
        let current = try await fetchCollectionRoom(accessToken: "fixture", collectionRoomID: "")
        guard tier > current.currentTier else { return current }
        return setFixtureTier(tier)
    }
}
