import UIKit

final class PhotoSelectionViewController: UIViewController {
    private let viewModel: PhotoSelectionViewModel
    private let photoPicker: PhotoPicking
    private let onDone: () -> Void

    private let titleLabel = UILabel()
    private let counterLabel = UILabel()
    private let messageLabel = UILabel()
    private let noticeLabel = UILabel()
    private let progressView = UIProgressView(progressViewStyle: .default)
    private let thumbnailStrip = UIStackView()
    private let scrollView = UIScrollView()
    private let primaryButton = UIButton(configuration: .filled())
    private let secondaryButton = UIButton(configuration: .plain())

    init(
        viewModel: PhotoSelectionViewModel,
        photoPicker: PhotoPicking = SystemPhotoPicker(),
        onDone: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.photoPicker = photoPicker
        self.onDone = onDone
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Photos"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        render(viewModel.state)
    }

    private func configureLayout() {
        titleLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        counterLabel.font = .museScaled(ofSize: 15, weight: .medium)
        counterLabel.adjustsFontForContentSizeCategory = true
        counterLabel.textColor = .secondaryLabel
        counterLabel.textAlignment = .center
        counterLabel.numberOfLines = 0

        messageLabel.font = .museScaled(ofSize: 15)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        noticeLabel.font = .museScaled(ofSize: 13)
        noticeLabel.adjustsFontForContentSizeCategory = true
        noticeLabel.textColor = .systemOrange
        noticeLabel.textAlignment = .center
        noticeLabel.numberOfLines = 0

        progressView.translatesAutoresizingMaskIntoConstraints = false
        progressView.widthAnchor.constraint(equalToConstant: 260).isActive = true
        progressView.isAccessibilityElement = true
        progressView.accessibilityLabel = "Upload progress"
        progressView.accessibilityTraits.insert(.updatesFrequently)

        thumbnailStrip.axis = .horizontal
        thumbnailStrip.spacing = 6
        thumbnailStrip.alignment = .center
        scrollView.addSubview(thumbnailStrip)
        thumbnailStrip.translatesAutoresizingMaskIntoConstraints = false
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.showsHorizontalScrollIndicator = false

        primaryButton.addTarget(self, action: #selector(handlePrimaryTapped), for: .touchUpInside)
        secondaryButton.addTarget(self, action: #selector(handleSecondaryTapped), for: .touchUpInside)
        for button in [primaryButton, secondaryButton] {
            button.translatesAutoresizingMaskIntoConstraints = false
            button.widthAnchor.constraint(greaterThanOrEqualToConstant: 260).isActive = true
        }

        let stack = UIStackView(arrangedSubviews: [
            titleLabel, counterLabel, messageLabel, scrollView, progressView,
            noticeLabel, primaryButton, secondaryButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24),
            scrollView.heightAnchor.constraint(equalToConstant: 76),
            scrollView.widthAnchor.constraint(equalTo: stack.widthAnchor),
            thumbnailStrip.topAnchor.constraint(equalTo: scrollView.topAnchor),
            thumbnailStrip.bottomAnchor.constraint(equalTo: scrollView.bottomAnchor),
            thumbnailStrip.leadingAnchor.constraint(equalTo: scrollView.leadingAnchor),
            thumbnailStrip.trailingAnchor.constraint(equalTo: scrollView.trailingAnchor),
            thumbnailStrip.heightAnchor.constraint(equalTo: scrollView.heightAnchor)
        ])
    }

    private var lastAnnouncedStateIdentity: String?

    // MARK: - Rendering

    private func render(_ state: PhotoSelectionViewModel.State) {
        counterLabel.text = viewModel.counterText
        noticeLabel.isHidden = true
        scrollView.isHidden = true
        progressView.isHidden = true
        secondaryButton.isHidden = true
        primaryButton.isHidden = false
        primaryButton.isEnabled = true
        navigationItem.hidesBackButton = false

        switch state {
        case .ready:
            titleLabel.text = "Choose photographs for this Room"
            messageLabel.text = viewModel.capacityMessage
            primaryButton.configuration?.title = viewModel.addPhotosActionTitle

        case .selecting:
            titleLabel.text = "Choose photographs for this Room"
            messageLabel.text = viewModel.capacityMessage
            primaryButton.configuration?.title = viewModel.addPhotosActionTitle
            primaryButton.isEnabled = false

        case .selected:
            titleLabel.text = "Ready to add"
            messageLabel.text = slotSummary()
            renderThumbnails(viewModel.selectedPhotos)
            scrollView.isHidden = false
            primaryButton.configuration?.title = viewModel.confirmActionTitle
            secondaryButton.isHidden = false
            secondaryButton.configuration?.title = "Choose Different Photos"

            let notices = [viewModel.failureNotice, viewModel.reflowNotice].compactMap { $0 }
            noticeLabel.isHidden = notices.isEmpty
            noticeLabel.text = notices.joined(separator: "\n")

        case .uploading(let completed, let total):
            titleLabel.text = "Saving to your Room"
            messageLabel.text = viewModel.uploadingMessage(completed: completed, total: total)
            renderThumbnails(viewModel.selectedPhotos)
            scrollView.isHidden = false
            progressView.isHidden = false
            progressView.setProgress(total == 0 ? 0 : Float(completed) / Float(total), animated: true)
            progressView.accessibilityValue = "\(completed) of \(total) saved"
            primaryButton.isHidden = true
            navigationItem.hidesBackButton = true

        case .committed(let outcome):
            titleLabel.text = outcome.hasFailures ? "Partly saved" : "Saved"
            messageLabel.text = viewModel.committedMessage(outcome)
            if outcome.hasFailures {
                renderThumbnails(viewModel.selectedPhotos, markAllFailed: true)
                scrollView.isHidden = false
                noticeLabel.isHidden = false
                noticeLabel.text = "Tap Retry to try the unsaved photographs again."
                primaryButton.configuration?.title = viewModel.retryActionTitle
                secondaryButton.isHidden = false
                secondaryButton.configuration?.title = "Done"
            } else {
                primaryButton.configuration?.title = "Done"
            }

        case .commitFailed:
            titleLabel.text = "Couldn't save"
            messageLabel.text = PhotoSelectionViewModel.commitFailedMessage
            renderThumbnails(viewModel.selectedPhotos)
            scrollView.isHidden = false
            primaryButton.configuration?.title = "Try Again"
            secondaryButton.isHidden = false
            secondaryButton.configuration?.title = "Choose Different Photos"

        case .roomFull:
            titleLabel.text = "Room full"
            messageLabel.text = viewModel.roomFullMessage
            primaryButton.isHidden = true
            secondaryButton.isHidden = false
            secondaryButton.configuration?.title = "Back to Rooms"
        }

        announceStateIfChanged(state)
    }

    private func announceStateIfChanged(_ state: PhotoSelectionViewModel.State) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity

        let headline = [titleLabel.text, messageLabel.text]
            .compactMap { $0 }
            .joined(separator: ". ")
        switch state {
        case .commitFailed:
            MuseAccessibility.announceFailure(headline)
        default:
            MuseAccessibility.announce(headline)
        }
    }

    private func slotSummary() -> String {
        let assignments = viewModel.newPhotoAssignments
        guard !assignments.isEmpty else { return viewModel.capacityMessage }
        var counts: [String: Int] = [:]
        for assignment in assignments { counts[assignment.slot.wall.rawValue, default: 0] += 1 }
        let summary = counts.sorted { $0.key < $1.key }
            .map { "\($0.value) \($0.key)" }
            .joined(separator: " · ")
        return "Wall placement: \(summary)"
    }

    private func renderThumbnails(_ photos: [PickedPhoto], markAllFailed: Bool = false) {
        thumbnailStrip.arrangedSubviews.forEach { $0.removeFromSuperview() }
        for photo in photos {
            thumbnailStrip.addArrangedSubview(makeThumbnail(photo, failed: markAllFailed))
        }
    }

    private func makeThumbnail(_ photo: PickedPhoto, failed: Bool) -> UIView {
        let imageView = UIImageView()
        imageView.contentMode = .scaleAspectFill
        imageView.clipsToBounds = true
        imageView.layer.cornerRadius = 6
        imageView.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            imageView.widthAnchor.constraint(equalToConstant: 68),
            imageView.heightAnchor.constraint(equalToConstant: 68)
        ])

        imageView.isAccessibilityElement = true
        imageView.accessibilityTraits.insert(.image)

        switch photo.loadState {
        case .ready(let data, _):
            imageView.image = UIImage(data: data)
            imageView.accessibilityLabel = "Selected photograph"
            if failed {
                imageView.alpha = 0.6
                imageView.layer.borderColor = UIColor.systemOrange.cgColor
                imageView.layer.borderWidth = 2
                imageView.accessibilityLabel = "Photograph not yet saved"
            }
        case .failed:
            imageView.backgroundColor = .secondarySystemFill
            imageView.image = UIImage(systemName: "exclamationmark.triangle")
            imageView.tintColor = .systemOrange
            imageView.contentMode = .center
            imageView.accessibilityLabel = "Photograph failed to load"
        }
        return imageView
    }

    // MARK: - Actions

    @objc private func handlePrimaryTapped() {
        switch viewModel.state {
        case .ready:
            presentPicker()
        case .selected:
            Task { await viewModel.commit() }
        case .committed(let outcome):
            if outcome.hasFailures {
                Task { await viewModel.retry() }
            } else {
                onDone()
            }
        case .commitFailed:
            Task { await viewModel.retry() }
        case .selecting, .uploading, .roomFull:
            break
        }
    }

    @objc private func handleSecondaryTapped() {
        switch viewModel.state {
        case .selected, .commitFailed:
            viewModel.reset()
            presentPicker()
        case .committed, .roomFull:
            onDone()
        case .ready, .selecting, .uploading:
            break
        }
    }

    private func presentPicker() {
        viewModel.beginSelection()
        guard case .selecting = viewModel.state else { return }
        Task { [weak self] in
            guard let self else { return }
            let picked = await photoPicker.pickPhotos(
                limit: viewModel.selectionLimit,
                presentingFrom: self
            )
            viewModel.ingest(picked)
        }
    }
}
