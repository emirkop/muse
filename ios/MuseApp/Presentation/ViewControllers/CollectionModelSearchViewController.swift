import UIKit

final class CollectionModelSearchViewController: UIViewController {
    private let viewModel: CollectionModelSearchViewModel
    private let onSelectModel: (CollectionCatalogModel) -> Void
    private var didSelectModel = false

    private let tableView = UITableView(frame: .zero, style: .insetGrouped)
    private let searchController = UISearchController(searchResultsController: nil)
    private let activityIndicator = UIActivityIndicatorView(style: .medium)

    private let statusView = UIView()
    private let statusLabel = UILabel()
    private let statusDetailLabel = UILabel()
    private let retryButton = UIButton(configuration: .bordered())

    private var models: [CollectionCatalogModel] = []
    private var canLoadMore = false

    private var searchDebounce: Task<Void, Never>?
    private static let debounceInterval = Duration.milliseconds(300)

    init(
        viewModel: CollectionModelSearchViewModel,
        onSelectModel: @escaping (CollectionCatalogModel) -> Void
    ) {
        self.viewModel = viewModel
        self.onSelectModel = onSelectModel
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    deinit {
        searchDebounce?.cancel()
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Search Catalog"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        viewModel.onActivityChange = { [weak self] isSearching in
            self?.renderActivity(isSearching)
        }
        render(viewModel.state)
        Task { await viewModel.search() }
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        searchController.searchBar.becomeFirstResponder()
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        if isMovingFromParent && !didSelectModel {
            viewModel.recordSearchAbandoned()
        }
    }

    private func configureLayout() {
        searchController.searchResultsUpdater = self
        searchController.obscuresBackgroundDuringPresentation = false
        searchController.searchBar.placeholder = "Brand or model"
        searchController.searchBar.accessibilityLabel = "Search the catalogue by brand or model"
        searchController.searchBar.autocapitalizationType = .none
        searchController.searchBar.text = viewModel.query
        navigationItem.searchController = searchController
        navigationItem.hidesSearchBarWhenScrolling = false

        tableView.dataSource = self
        tableView.delegate = self
        tableView.register(UITableViewCell.self, forCellReuseIdentifier: Self.cellIdentifier)
        tableView.keyboardDismissMode = .onDrag
        tableView.translatesAutoresizingMaskIntoConstraints = false

        configureStatusView()

        activityIndicator.hidesWhenStopped = true
        activityIndicator.translatesAutoresizingMaskIntoConstraints = false
        activityIndicator.isAccessibilityElement = true
        activityIndicator.accessibilityLabel = "Searching"

        view.addSubview(tableView)
        view.addSubview(statusView)
        view.addSubview(activityIndicator)

        NSLayoutConstraint.activate([
            tableView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            tableView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            tableView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            tableView.bottomAnchor.constraint(equalTo: view.bottomAnchor),

            statusView.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 32),
            statusView.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -32),
            statusView.centerYAnchor.constraint(equalTo: view.centerYAnchor),

            activityIndicator.topAnchor.constraint(
                equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 12
            ),
            activityIndicator.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -20),
        ])
    }

    private func configureStatusView() {
        statusLabel.font = .museScaled(ofSize: 17, weight: .semibold)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .label
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0
        statusLabel.museMarkAsHeader()

        statusDetailLabel.font = .museScaled(ofSize: 15)
        statusDetailLabel.adjustsFontForContentSizeCategory = true
        statusDetailLabel.textColor = .secondaryLabel
        statusDetailLabel.textAlignment = .center
        statusDetailLabel.numberOfLines = 0

        retryButton.setTitle("Try Again", for: .normal)
        retryButton.accessibilityHint = "Runs the search again"
        retryButton.addTarget(self, action: #selector(retryTapped), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [statusLabel, statusDetailLabel, retryButton])
        stack.axis = .vertical
        stack.alignment = .center
        stack.spacing = 12
        stack.translatesAutoresizingMaskIntoConstraints = false

        statusView.addSubview(stack)
        statusView.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: statusView.topAnchor),
            stack.bottomAnchor.constraint(equalTo: statusView.bottomAnchor),
            stack.leadingAnchor.constraint(equalTo: statusView.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: statusView.trailingAnchor),
        ])
    }

    // MARK: - Rendering

    private func render(_ state: CollectionModelSearchViewModel.State) {
        switch state {
        case .searching:
            models = []
            canLoadMore = false
            tableView.isHidden = true
            statusView.isHidden = true

        case .results(let loaded, let more):
            models = loaded
            canLoadMore = more
            tableView.isHidden = false
            statusView.isHidden = true
            tableView.reloadData()
            if let message = viewModel.lastPageErrorMessage {
                presentPageFailure(message)
            }

        case .noResults(let query):
            models = []
            canLoadMore = false
            tableView.isHidden = true
            statusView.isHidden = false
            statusLabel.text = query.isEmpty
                ? "No models in this category yet"
                : "No matches for \"\(query)\""
            statusDetailLabel.text = query.isEmpty
                ? "The catalog for this category hasn't been published yet."
                : "Try a different brand or model name."
            retryButton.isHidden = true

        case .failed(let message):
            models = []
            canLoadMore = false
            tableView.isHidden = true
            statusView.isHidden = false
            statusLabel.text = "Search unavailable"
            statusDetailLabel.text = message
            retryButton.isHidden = false
        }

        announceStateIfChanged(state)
    }

    private func announceStateIfChanged(_ state: CollectionModelSearchViewModel.State) {
        let identity = String(describing: state)
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity

        switch state {
        case .searching:
            break
        case .results(let loaded, _):
            MuseAccessibility.announce("\(loaded.count) result\(loaded.count == 1 ? "" : "s")")
        case .noResults:
            MuseAccessibility.announce(
                [statusLabel.text, statusDetailLabel.text].compactMap { $0 }.joined(separator: ". ")
            )
        case .failed:
            MuseAccessibility.announceFailure(
                [statusLabel.text, statusDetailLabel.text].compactMap { $0 }.joined(separator: ". ")
            )
        }
    }

    private func renderActivity(_ isSearching: Bool) {
        if isSearching {
            activityIndicator.startAnimating()
        } else {
            activityIndicator.stopAnimating()
        }
        activityIndicator.isAccessibilityElement = isSearching
    }

    private func presentPageFailure(_ message: String) {
        let alert = UIAlertController(title: "Couldn't load more", message: message, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "OK", style: .default))
        present(alert, animated: true)
    }

    @objc private func retryTapped() {
        Task { await viewModel.search() }
    }

    private static let cellIdentifier = "CollectionModelSearchCell"
    private var lastAnnouncedStateIdentity: String?
}

// MARK: - Search field

extension CollectionModelSearchViewController: UISearchResultsUpdating {
    func updateSearchResults(for searchController: UISearchController) {
        let text = searchController.searchBar.text ?? ""
        searchDebounce?.cancel()
        searchDebounce = Task { [weak self] in
            try? await Task.sleep(for: Self.debounceInterval)
            guard !Task.isCancelled, let self else { return }
            await self.viewModel.updateQuery(text)
        }
    }
}

// MARK: - Results

extension CollectionModelSearchViewController: UITableViewDataSource, UITableViewDelegate {
    func tableView(_ tableView: UITableView, numberOfRowsInSection section: Int) -> Int {
        models.count
    }

    func tableView(_ tableView: UITableView, titleForHeaderInSection section: Int) -> String? {
        guard let name = viewModel.categoryDisplayName else { return nil }
        return "In \(name)"
    }

    func tableView(_ tableView: UITableView, cellForRowAt indexPath: IndexPath) -> UITableViewCell {
        let cell = tableView.dequeueReusableCell(withIdentifier: Self.cellIdentifier, for: indexPath)
        let model = models[indexPath.row]

        var configuration = UIListContentConfiguration.subtitleCell()
        configuration.text = model.displayName
        var subtitle = model.brandDisplayName
        if model.isDevelopmentFixture {
            subtitle += " · Development fixture — not a real product"
        }
        configuration.secondaryText = subtitle
        cell.contentConfiguration = configuration
        cell.accessoryType = .disclosureIndicator
        cell.accessibilityLabel = "\(model.displayName), \(subtitle)"
        cell.accessibilityHint = "Opens confirmation for this model"
        return cell
    }

    func tableView(_ tableView: UITableView, didSelectRowAt indexPath: IndexPath) {
        tableView.deselectRow(at: indexPath, animated: true)
        guard let model = viewModel.select(modelID: models[indexPath.row].id) else { return }
        didSelectModel = true
        onSelectModel(model)
    }

    func tableView(
        _ tableView: UITableView, willDisplay cell: UITableViewCell, forRowAt indexPath: IndexPath
    ) {
        guard canLoadMore, indexPath.row == models.count - 1 else { return }
        Task { await viewModel.loadMore() }
    }
}
