import UIKit

@MainActor
public final class AppCoordinator {
    // MARK: - Properties

    private let window: UIWindow
    private let navigationController = UINavigationController()
    private let sessionStore: SessionStoring
    private let apiClient: IdentityAPIClient
    private let museumClient: MuseumAPIClient
    private let collectionClient: CollectionAPIClient
    private let photoUploader: UploadRoomPhotosUseCase
    private let photoAPIClient: PhotoUploadAPIClient
    private let musicPlayer = AVAudioRoomMusicPlayer()
    private let accessTokenBox: AccessTokenBox
    private var currentAccessToken: String? {
        get { accessTokenBox.value }
        set { accessTokenBox.value = newValue }
    }
    private let shareLinkClient: ShareLinkAPIClient
    private let entitlementClient: EntitlementAPIClient
    private let capacityStore = AppStoreCapacityStore()
    private var appStoreUpdates: Task<Void, Never>?
    private let assetBundleClient: AssetBundleAPIClient
    private let assetBundles: AssetBundleDeliveryService
    private let variantLayouts: DeliveredVariantLayoutProvider
    private let previewAssets: DeliveredPreviewAssetProvider
    private let assetCache: AssetBundleCache
    private let activeBundles: ActiveBundleRegistry
    private let ownedContentPins: OwnedContentPinner
    private var pendingShareLink: MuseShareLink?
    private var launchRoutingComplete = false
    private weak var launchScreen: LaunchLoadingViewController?

    private let diagnostics: any ErrorReporting

    private lazy var analytics: any AnalyticsRecording = AnalyticsRecorder(
        client: AnalyticsAPIClient(baseURL: AppEnvironment.current.apiBaseURL),
        accessToken: { [box = accessTokenBox] in box.value }
    )

    // MARK: - Initialization

    public init(window: UIWindow) {
        self.window = window
        self.sessionStore = KeychainSessionStore()
        self.apiClient = IdentityAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        self.museumClient = MuseumAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        self.collectionClient = CollectionAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        self.shareLinkClient = ShareLinkAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        self.entitlementClient = EntitlementAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        self.photoAPIClient = PhotoUploadAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        self.photoUploader = UploadRoomPhotosUseCase(
            photoService: photoAPIClient,
            uploader: URLSessionObjectUploader(),
            removeSpooledFile: { PhotoUploadSpool.shared.remove($0) }
        )

        let assetBundleClient = AssetBundleAPIClient(baseURL: AppEnvironment.current.apiBaseURL)
        let activeBundles = ActiveBundleRegistry()
        let diagnostics: any ErrorReporting = AppEnvironment.current == .development
            ? ConsoleErrorReporting()
            : NoErrorReporting()
        self.diagnostics = diagnostics
        let assetCache = AssetBundleCache(
            policy: .developmentDefault, retention: activeBundles, diagnostics: diagnostics)
        let assetBundles = AssetBundleDeliveryService(
            manifests: assetBundleClient, cache: assetCache, diagnostics: diagnostics)
        self.assetBundleClient = assetBundleClient
        self.activeBundles = activeBundles
        self.assetCache = assetCache
        self.assetBundles = assetBundles

        let accessTokenBox = AccessTokenBox()
        self.accessTokenBox = accessTokenBox
        let tokenProvider: @Sendable () async -> String? = { accessTokenBox.value }
        let variantLayouts = DeliveredVariantLayoutProvider(
            bundles: assetBundles,
            variants: assetBundleClient,
            accessToken: tokenProvider
        )
        self.variantLayouts = variantLayouts
        self.ownedContentPins = OwnedContentPinner(museums: museumClient, catalog: museumClient, retention: activeBundles)
        self.previewAssets = DeliveredPreviewAssetProvider(
            bundles: assetBundles,
            accessToken: tokenProvider
        )
    }

    // MARK: - Public Methods

    public func start() {
        PhotoUploadSpool.shared.purgeAll()

        let assetCache = self.assetCache
        Task.detached(priority: .utility) { await assetCache.reconcile() }

        window.rootViewController = navigationController
        window.makeKeyAndVisible()

        let launchScreen = LaunchLoadingViewController()
        launchScreen.onRetry = { [weak self] in
            guard let self else { return }
            Task { await self.routeAtLaunch() }
        }
        self.launchScreen = launchScreen
        navigationController.setViewControllers([launchScreen], animated: false)
        Task { await routeAtLaunch() }
    }

    // MARK: - Launch & Session Routing

    private func routeAtLaunch() async {
        guard let refreshToken = try? sessionStore.loadRefreshToken() else {
            landAfterLaunch { showFirstLaunch() }
            return
        }

        do {
            let session = try await apiClient.refreshSession(refreshToken: refreshToken)
            try sessionStore.save(session)
            currentAccessToken = session.accessToken
            startForwardingAppStoreTransactions()
            landAfterLaunch { showMainProductChoice() }
        } catch where LaunchSessionRouting.verdict(for: error) == .serverUnreachable {
            launchScreen?.showServerUnreachable(
                message: NetworkFailureCopy.message(
                    for: error,
                    operation: .read,
                    otherwise: "Couldn't reach Muse. Check your connection and try again."
                )
            )
        } catch {
            try? sessionStore.clear()
            landAfterLaunch { showAuthentication() }
        }
    }

    private func landAfterLaunch(otherwise: () -> Void) {
        launchRoutingComplete = true
        if pendingShareLink != nil {
            presentPendingShareLink()
        } else {
            otherwise()
        }
    }

    private func showFirstLaunch() {
        let viewController = FirstLaunchViewController { [weak self] in
            self?.showAuthentication()
        }
        navigationController.setViewControllers([viewController], animated: false)
    }

    private func showAuthentication() {
        navigationController.setViewControllers([makeLogIn(prefilledEmail: "")], animated: false)
    }

    private func makeProviderViewModel() -> AuthenticationViewModel {
        AuthenticationViewModel(
            appleSignInUseCase: SignInWithAppleUseCase(
                identityProvider: AppleSignInProvider(),
                authService: apiClient,
                sessionStore: sessionStore
            ),
            googleSignInUseCase: SignInWithGoogleUseCase(
                identityProvider: GoogleSignInProvider(),
                authService: apiClient,
                sessionStore: sessionStore
            )
        )
    }

    private func makeEmailAuthViewModel() -> EmailAuthViewModel {
        EmailAuthViewModel(authService: apiClient, sessionStore: sessionStore)
    }

    private func routeAuthenticated(_ result: LoginResult) {
        currentAccessToken = result.session.accessToken
        startForwardingAppStoreTransactions()
        switch DeepLinkRouting.destinationAfterAuthentication(
            pendingShareLink: pendingShareLink,
            isNewAccount: result.isNewAccount
        ) {
        case .accountCreation:
            showAccountCreation()
        case .sharedMuseumLanding, .sharedCollectionRoomLanding:
            presentPendingShareLink()
        case .mainHub:
            showMainProductChoice()
        }
    }

    private func landAfterOnboarding() {
        switch DeepLinkRouting.destinationAfterOnboarding(pendingShareLink: pendingShareLink) {
        case .sharedMuseumLanding, .sharedCollectionRoomLanding:
            presentPendingShareLink()
        case .accountCreation, .mainHub:
            showMainProductChoice()
        }
    }

    private func makeLogIn(prefilledEmail: String) -> UIViewController {
        LogInViewController(
            emailViewModel: makeEmailAuthViewModel(),
            providerViewModel: makeProviderViewModel(),
            prefilledEmail: prefilledEmail,
            onSignedIn: { [weak self] result in self?.routeAuthenticated(result) },
            onSignUp: { [weak self] in self?.showSignUp(prefilledEmail: prefilledEmail) },
            onForgotPassword: { [weak self] email in self?.showPasswordReset(prefilledEmail: email) }
        )
    }

    private func showSignUp(prefilledEmail: String) {
        let logIn = navigationController.viewControllers.compactMap { $0 as? LogInViewController }.last
        let carried = logIn?.enteredEmail.isEmpty == false ? logIn!.enteredEmail : prefilledEmail

        let controller = SignUpViewController(
            emailViewModel: makeEmailAuthViewModel(),
            providerViewModel: makeProviderViewModel(),
            prefilledEmail: carried,
            onSignedIn: { [weak self] result in self?.routeAuthenticated(result) },
            onVerificationSent: { [weak self] email in self?.showVerificationPending(email: email) },
            onLogIn: { [weak self] in self?.popToLogIn() }
        )
        navigationController.pushViewController(controller, animated: true)
    }

    private func showVerificationPending(email: String) {
        let controller = VerificationPendingViewController(
            viewModel: makeEmailAuthViewModel(),
            email: email,
            onVerified: { [weak self] result in self?.routeAuthenticated(result) },
            onBackToLogIn: { [weak self] in self?.popToLogIn() }
        )
        navigationController.pushViewController(controller, animated: true)
    }

    private func showPasswordReset(prefilledEmail: String) {
        let controller = PasswordResetViewController(
            viewModel: makeEmailAuthViewModel(),
            prefilledEmail: prefilledEmail,
            onFinished: { [weak self] in self?.popToLogIn() }
        )
        navigationController.pushViewController(controller, animated: true)
    }

    private func popToLogIn() {
        if let logIn = navigationController.viewControllers.first(where: { $0 is LogInViewController }) {
            navigationController.popToViewController(logIn, animated: true)
        } else {
            navigationController.setViewControllers([makeLogIn(prefilledEmail: "")], animated: true)
        }
    }

    private func showAccountCreation() {
        let viewController = AccountCreationViewController { [weak self] in
            self?.showFirstTimeAvatarSelection()
        }
        navigationController.setViewControllers([viewController], animated: true)
    }

    private func showFirstTimeAvatarSelection() {
        guard let currentAccessToken else { return }
        let viewModel = AvatarSelectionViewModel(profileService: apiClient, accessToken: currentAccessToken)
        let viewController = AvatarSelectionViewController(viewModel: viewModel, currentAvatarID: nil) { [weak self] _ in
            self?.landAfterOnboarding()
        }
        navigationController.setViewControllers([viewController], animated: true)
    }

    private func showMainProductChoice() {
        let viewController = MainProductChoiceViewController(
            onSelectMuseum: { [weak self] in self?.showMuseumEntry() },
            onSelectCollectionRooms: { [weak self] in self?.showCollectionRoomList() },
            onViewProfile: { [weak self] in self?.showOwnProfile() }
        )
        navigationController.setViewControllers([viewController], animated: false)
    }

    // MARK: - Collection Rooms

    private func showCollectionRoomList() {
        guard let currentAccessToken else { return }
        let viewModel = CollectionRoomListViewModel(
            collections: collectionClient,
            catalog: collectionClient,
            accessToken: currentAccessToken
        )
        let viewController = CollectionRoomListViewController(
            viewModel: viewModel,
            onCreate: { [weak self] in self?.showCollectionRoomCreation(insertInto: viewModel) },
            onSelectRoom: { [weak self] room in
                self?.showCollectionDesignSelection(room: room) { _ in
                }
            },
            onAddItem: { [weak self] room in
                self?.showCollectionModelSearch(
                    room: room,
                    categoryDisplayName: viewModel.categoryName(for: room)
                )
            },
            onShare: { [weak self] room in self?.shareCollectionRoom(room) },
            onMusic: { [weak self] room in self?.showCollectionRoomMusic(room: room) }
        )
        navigationController.pushViewController(viewController, animated: true)
    }

    private func showCollectionRoomCreation(insertInto list: CollectionRoomListViewModel) {
        guard let currentAccessToken else { return }
        let viewModel = CollectionRoomCreationViewModel(
            catalog: collectionClient,
            collections: collectionClient,
            accessToken: currentAccessToken,
            analytics: analytics
        )
        let viewController = CollectionRoomCreationViewController(viewModel: viewModel) { [weak self] room in
            list.insert(room)
            self?.navigationController.popViewController(animated: true)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    private func showCollectionDesignSelection(
        room: CollectionRoom,
        onApplied: @escaping (CollectionRoom) -> Void
    ) {
        guard let currentAccessToken else { return }
        let viewModel = CollectionDesignSelectionViewModel(
            room: room,
            catalog: collectionClient,
            collections: collectionClient,
            accessToken: currentAccessToken
        )
        var controller: CollectionDesignSelectionViewController?
        controller = CollectionDesignSelectionViewController(
            viewModel: viewModel,
            onPreviewDesign: { [weak self] design in
                self?.showPreview(
                    subject: design.previewSubject,
                    isCurrentlySelected: design.id == viewModel.selectedDesignID,
                    confirmationReassurance: CollectionDesignSelectionViewModel.previewReassurance,
                    backButtonTitle: "Back to Designs",
                    onChoose: { designID in controller?.applyDesign(id: designID) }
                )
            },
            onApplied: { [weak self] updated in
                onApplied(updated)
                self?.navigationController.popViewController(animated: true)
            }
        )
        navigationController.pushViewController(controller!, animated: true)
    }

    private func showCollectionModelSearch(room: CollectionRoom, categoryDisplayName: String?) {
        guard let currentAccessToken, let categoryID = room.categoryID else { return }
        Task { @MainActor [weak self] in
            guard let self else { return }
            if let entitlement = try? await self.entitlementClient.fetchEntitlement(accessToken: currentAccessToken),
               entitlement.isAtCapacity {
                self.showCapacityReached()
                return
            }
            self.pushCollectionModelSearch(room: room, categoryID: categoryID, categoryDisplayName: categoryDisplayName, accessToken: currentAccessToken)
        }
    }

    private func pushCollectionModelSearch(room: CollectionRoom, categoryID: String, categoryDisplayName: String?, accessToken currentAccessToken: String) {
        let viewModel = CollectionModelSearchViewModel(
            categoryID: categoryID,
            categoryDisplayName: categoryDisplayName,
            catalog: collectionClient,
            accessToken: currentAccessToken,
            analytics: analytics
        )
        let controller = CollectionModelSearchViewController(viewModel: viewModel) { [weak self] model in
            self?.showCollectionItemConfirmation(model: model, room: room)
        }
        navigationController.pushViewController(controller, animated: true)
    }

    private func showCollectionItemConfirmation(model: CollectionCatalogModel, room: CollectionRoom) {
        guard let currentAccessToken else { return }
        let viewModel = CollectionItemAdditionViewModel(
            model: model,
            room: room,
            addition: CollectionItemAddition(
                expansion: CollectionTierExpansion(
                    ratchet: collectionClient,
                    geometry: DeliveredCollectionTierGeometry(bundles: assetBundles)
                ),
                items: collectionClient
            ),
            tables: DeliveredCollectionDesignTableProvider(
                bundles: assetBundles,
                catalog: collectionClient
            ),
            accessToken: currentAccessToken
        )
        let controller = CollectionItemConfirmationViewController(
            viewModel: viewModel,
            roomName: room.name,
            onAdded: { _ in
            }
        )
        controller.onCapacityReached = { [weak self] in self?.showCapacityReached() }
        navigationController.pushViewController(controller, animated: true)
    }

    // MARK: - Collection capacity & purchase

    private func showCapacityReached() {
        guard let currentAccessToken else { return }
        let store = capacityStore
        let viewModel = CapacityViewModel(
            entitlements: entitlementClient,
            store: store,
            productID: AppEnvironment.current.collectionCapacityProductID,
            accessToken: currentAccessToken,
            finish: { signed in await store.finish(signedTransaction: signed) },
            analytics: analytics
        )
        let controller = CapacityReachedViewController(
            viewModel: viewModel,
            onUpgraded: { _ in },
            onDismiss: { [weak self] in self?.navigationController.popViewController(animated: true) }
        )
        navigationController.pushViewController(controller, animated: true)
    }

    private func startForwardingAppStoreTransactions() {
        guard appStoreUpdates == nil else { return }
        let client = entitlementClient
        let store = capacityStore
        let tokenBox = accessTokenBox
        appStoreUpdates = store.observeUpdates { signed in
            guard let token = tokenBox.value else { return }
            if (try? await client.redeem(accessToken: token, signedTransaction: signed)) != nil {
                await store.finish(signedTransaction: signed)
            }
        }
    }

    // MARK: - Museum

    private func refreshOwnedContentPins(accessToken: String) {
        let pins = ownedContentPins
        Task.detached(priority: .utility) { _ = await pins.refresh(accessToken: accessToken) }
    }

    private func showMuseumEntry() {
        guard let currentAccessToken else { return }
        refreshOwnedContentPins(accessToken: currentAccessToken)
        let viewModel = MuseumEntryViewModel(museumService: museumClient, accessToken: currentAccessToken)
        let viewController = MuseumEntryViewController(
            viewModel: viewModel,
            onCreateMuseum: { [weak self] in self?.showMuseumCreation() },
            onEnterMuseum: { [weak self] museum in self?.showRoomList(museum: museum) },
            onChangeStyle: { [weak self] museum in
                self?.showStyleSelection(context: .changingStyle(currentStyleID: museum.styleID))
            },
            onOpenPrivacy: { [weak self] _ in self?.showPrivacySettings() },
            onShareMuseum: { [weak self] museum in self?.shareMuseum(museum) },
            onRegenerateLink: { [weak self] museum in self?.regenerateShareLink(for: museum) }
        )
        navigationController.pushViewController(viewController, animated: true)
    }

    // MARK: - Sharing

    private func shareMuseum(_ museum: Museum) {
        guard let currentAccessToken else { return }
        let viewModel = MuseumSharingViewModel(shareLinkService: shareLinkClient, accessToken: currentAccessToken)
        Task { @MainActor [weak self] in
            guard let self else { return }
            switch await viewModel.shareLink(for: museum) {
            case .museumIsPrivate:
                self.presentPrivateMuseumSharingNotice()
            case .link(let link):
                self.presentShareSheet(for: link.url)
            case .failed(let message):
                self.presentSimpleAlert(title: "Share Museum", message: message)
            }
        }
    }

    private func regenerateShareLink(for museum: Museum) {
        guard let currentAccessToken else { return }
        let viewModel = MuseumSharingViewModel(shareLinkService: shareLinkClient, accessToken: currentAccessToken)
        let alert = UIAlertController(
            title: "Create a new link?",
            message: "Anyone with your current link will no longer be able to use it.",
            preferredStyle: .alert
        )
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        alert.addAction(UIAlertAction(title: "New Link", style: .destructive) { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self else { return }
                switch await viewModel.regenerateLink() {
                case .link(let link):
                    if MuseumPrivacyRules.museumIsReachableByVisitors(museum.privacy) {
                        self.presentShareSheet(for: link.url)
                    } else {
                        self.presentSimpleAlert(
                            title: "New link created",
                            message: "Your old link no longer works. Your Museum is Private, so no one can enter until you make it Public."
                        )
                    }
                case .failed(let message):
                    self.presentSimpleAlert(title: "New Link", message: message)
                case .museumIsPrivate:
                    break
                }
            }
        })
        navigationController.present(alert, animated: true)
    }

    private func presentPrivateMuseumSharingNotice() {
        let alert = UIAlertController(
            title: "Your Museum is Private",
            message: "Visitors won't be able to enter until you make it Public. Change that in Privacy, then share.",
            preferredStyle: .alert
        )
        alert.addAction(UIAlertAction(title: "Open Privacy", style: .default) { [weak self] _ in self?.showPrivacySettings() })
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        navigationController.present(alert, animated: true)
    }

    private func presentShareSheet(for url: URL) {
        let sheet = UIActivityViewController(activityItems: [url], applicationActivities: nil)
        if let popover = sheet.popoverPresentationController {
            popover.sourceView = navigationController.view
            popover.sourceRect = CGRect(x: navigationController.view.bounds.midX, y: navigationController.view.bounds.midY, width: 0, height: 0)
            popover.permittedArrowDirections = []
        }
        navigationController.present(sheet, animated: true)
    }

    private func presentSimpleAlert(title: String, message: String) {
        let alert = UIAlertController(title: title, message: message, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "OK", style: .default))
        navigationController.present(alert, animated: true)
    }

    // MARK: - Incoming share links

    @discardableResult
    public func handleIncomingURL(_ url: URL) -> Bool {
        guard let link = MuseShareLinkURL.parse(url, acceptedHosts: AppEnvironment.current.shareLinkHosts) else {
            return false
        }
        pendingShareLink = link
        if launchRoutingComplete {
            presentPendingShareLink()
        }
        return true
    }

    private func presentPendingShareLink() {
        guard let pending = pendingShareLink else { return }
        let landing: UIViewController
        switch pending {
        case .museum(let code):
            let access: ShareLinkLandingViewModel.Access = currentAccessToken.map { .signedIn(accessToken: $0) } ?? .signedOut
            let viewModel = ShareLinkLandingViewModel(code: code, access: access, shareLinkService: shareLinkClient)
            landing = ShareLinkLandingViewController(
                viewModel: viewModel,
                onSignIn: { [weak self] in self?.showAuthentication() },
                onEntered: { [weak self] content in self?.enterSharedMuseum(content) },
                onDismiss: { [weak self] in self?.dismissPendingShareLink() }
            )
        case .collectionRoom(let code):
            let access: CollectionShareLinkLandingViewModel.Access = currentAccessToken.map { .signedIn(accessToken: $0) } ?? .signedOut
            let viewModel = CollectionShareLinkLandingViewModel(code: code, access: access, sharedRooms: collectionClient)
            landing = CollectionShareLinkLandingViewController(
                viewModel: viewModel,
                onSignIn: { [weak self] in self?.showAuthentication() },
                onEntered: { [weak self] content in self?.enterSharedCollectionRoom(content) },
                onDismiss: { [weak self] in self?.dismissPendingShareLink() }
            )
        }
        if currentAccessToken == nil {
            navigationController.setViewControllers([landing], animated: false)
        } else {
            if !(navigationController.viewControllers.first is MainProductChoiceViewController) {
                showMainProductChoice()
            }
            var stack = navigationController.viewControllers
            if stack.last is ShareLinkLandingViewController || stack.last is CollectionShareLinkLandingViewController {
                stack.removeLast()
            }
            stack.append(landing)
            navigationController.setViewControllers(stack, animated: true)
        }
    }

    private func dismissPendingShareLink() {
        pendingShareLink = nil
        if currentAccessToken == nil {
            showAuthentication()
        } else if navigationController.viewControllers.count > 1 {
            navigationController.popViewController(animated: true)
        } else {
            showMainProductChoice()
        }
    }

    private func enterSharedMuseum(_ content: SharedMuseumContent) {
        guard case .museum(let code)? = pendingShareLink, let currentAccessToken else { return }
        Task { @MainActor [weak self] in
            guard let self else { return }
            let own = try? await self.museumClient.fetchMuseum(accessToken: currentAccessToken)
            switch DeepLinkRouting.sharedMuseumEntry(sharedMuseumID: content.museumID, ownMuseumID: own?.id) {
            case .ownMuseum:
                self.pendingShareLink = nil
                self.showMainProductChoice()
                self.showMuseumEntry()
            case .visitor:
                let profile = try? await self.apiClient.fetchOwnProfile(accessToken: currentAccessToken)
                self.continueVisitorEntry(code: code, avatarID: profile?.avatarID ?? "")
            }
        }
    }

    private func continueVisitorEntry(code: String, avatarID: String) {
        if DeepLinkRouting.requiresAvatarSelection(avatarID: avatarID) {
            showAvatarSelectionBeforeVisiting(code: code)
            return
        }
        pendingShareLink = nil
        showVisitorLobby(code: code)
    }

    private func showAvatarSelectionBeforeVisiting(code: String) {
        guard let currentAccessToken else { return }
        let viewModel = AvatarSelectionViewModel(profileService: apiClient, accessToken: currentAccessToken)
        let viewController = AvatarSelectionViewController(viewModel: viewModel, currentAvatarID: nil) { [weak self] _ in
            guard let self else { return }
            self.pendingShareLink = nil
            self.showVisitorLobby(code: code)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    // MARK: - Shared Collection Rooms

    private func enterSharedCollectionRoom(_ content: SharedCollectionRoomContent) {
        guard case .collectionRoom(let code)? = pendingShareLink, let currentAccessToken else { return }
        Task { @MainActor [weak self] in
            guard let self else { return }
            let ownRoom = try? await self.collectionClient.fetchCollectionRoom(
                accessToken: currentAccessToken, collectionRoomID: content.collectionRoomID
            )
            if ownRoom != nil {
                self.pendingShareLink = nil
                self.showMainProductChoice()
                self.showCollectionRoomList()
                return
            }
            let profile = try? await self.apiClient.fetchOwnProfile(accessToken: currentAccessToken)
            self.continueCollectionVisitorEntry(code: code, content: content, avatarID: profile?.avatarID ?? "")
        }
    }

    private func continueCollectionVisitorEntry(code: String, content: SharedCollectionRoomContent, avatarID: String) {
        if DeepLinkRouting.requiresAvatarSelection(avatarID: avatarID) {
            showAvatarSelectionBeforeVisitingCollectionRoom(code: code, content: content)
            return
        }
        pendingShareLink = nil
        showSharedCollectionRoom(content)
    }

    private func showAvatarSelectionBeforeVisitingCollectionRoom(code: String, content: SharedCollectionRoomContent) {
        guard let currentAccessToken else { return }
        let viewModel = AvatarSelectionViewModel(profileService: apiClient, accessToken: currentAccessToken)
        let viewController = AvatarSelectionViewController(viewModel: viewModel, currentAvatarID: nil) { [weak self] _ in
            guard let self else { return }
            self.pendingShareLink = nil
            self.showSharedCollectionRoom(content)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    private func showSharedCollectionRoom(_ content: SharedCollectionRoomContent) {
        replaceTopViewController(with: SharedCollectionRoomViewController(
            content: content,
            musicCatalog: museumClient,
            musicPlayer: musicPlayer,
            accessToken: currentAccessToken
        ))
    }

    private func shareCollectionRoom(_ room: CollectionRoom) {
        guard let currentAccessToken else { return }
        let viewModel = CollectionSharingViewModel(
            shareLinks: collectionClient, accessToken: currentAccessToken, collectionRoomID: room.id
        )
        let sheet = UIAlertController(
            title: "Share “\(room.name)”",
            message: "Anyone with the link who signs in to Muse can view this Collection Room — only this Room, nothing else you own.",
            preferredStyle: .actionSheet
        )
        sheet.addAction(UIAlertAction(title: "Share Link", style: .default) { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self else { return }
                switch await viewModel.shareLink() {
                case .link(let link): self.presentShareSheet(for: link.url)
                case .failed(let message): self.presentSimpleAlert(title: "Share Collection Room", message: message)
                }
            }
        })
        sheet.addAction(UIAlertAction(title: "New Link…", style: .default) { [weak self] _ in
            self?.confirmRegenerateCollectionShareLink(viewModel)
        })
        sheet.addAction(UIAlertAction(title: "Stop Sharing…", style: .destructive) { [weak self] _ in
            self?.confirmStopSharingCollectionRoom(viewModel, roomName: room.name)
        })
        sheet.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        if let popover = sheet.popoverPresentationController {
            popover.sourceView = navigationController.view
            popover.sourceRect = CGRect(x: navigationController.view.bounds.midX, y: navigationController.view.bounds.midY, width: 0, height: 0)
            popover.permittedArrowDirections = []
        }
        navigationController.present(sheet, animated: true)
    }

    private func confirmRegenerateCollectionShareLink(_ viewModel: CollectionSharingViewModel) {
        let alert = UIAlertController(
            title: "Create a new link?",
            message: "Anyone with the current link will no longer be able to use it.",
            preferredStyle: .alert
        )
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        alert.addAction(UIAlertAction(title: "New Link", style: .destructive) { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self else { return }
                switch await viewModel.regenerateLink() {
                case .link(let link): self.presentShareSheet(for: link.url)
                case .failed(let message): self.presentSimpleAlert(title: "New Link", message: message)
                }
            }
        })
        navigationController.present(alert, animated: true)
    }

    private func confirmStopSharingCollectionRoom(_ viewModel: CollectionSharingViewModel, roomName: String) {
        let alert = UIAlertController(
            title: "Stop sharing “\(roomName)”?",
            message: "The current link will stop working. You can share again later with a new link.",
            preferredStyle: .alert
        )
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        alert.addAction(UIAlertAction(title: "Stop Sharing", style: .destructive) { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self else { return }
                switch await viewModel.stopSharing() {
                case .revoked: self.presentSimpleAlert(title: "Sharing stopped", message: "The link no longer works.")
                case .failed(let message): self.presentSimpleAlert(title: "Stop Sharing", message: message)
                }
            }
        })
        navigationController.present(alert, animated: true)
    }

    // MARK: - The visitor experience

    private func showVisitorLobby(code: String) {
        guard let currentAccessToken else { return }
        let viewModel = LobbyEntryViewModel(
            viewerRole: .visitor,
            contentSource: SharedMuseumLobbyContent(shareLinkService: shareLinkClient, code: code),
            geometry: UnavailableLobbyGeometryProvider(),
            cardTables: UnavailableLobbyCardTableProvider(),
            accessToken: currentAccessToken
        )
        let viewController = LobbyEntryViewController(
            viewModel: viewModel,
            onEnterLobby: { [weak self] content in
                guard let self else { return }
                let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime(diagnostics: diagnostics)
                let lobby = runtime.makeLobbyViewController(content: content) { [weak self] roomID in
                    self?.enterVisitorRoom(code: code, roomID: roomID)
                }
                replaceTopViewController(with: lobby)
            },
            onEnterRoomDirectly: { [weak self] room in
                guard let self else { return }
                var stack = navigationController.viewControllers
                stack.removeLast()
                navigationController.setViewControllers(stack, animated: false)
                showVisitorRoom(code: code, room: room)
            },
            onManageRooms: { [weak self] in self?.leaveVisit() }
        )
        navigationController.pushViewController(viewController, animated: true)
    }

    private func enterVisitorRoom(code: String, roomID: String) {
        guard let currentAccessToken else { return }
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let shared = try await shareLinkClient.sharedRoom(
                    accessToken: currentAccessToken, code: code, roomID: roomID)
                showVisitorRoom(code: code, room: shared.room)
            } catch {
                presentSimpleAlert(
                    title: "Not available",
                    message: "This Room isn't available."
                )
            }
        }
    }

    private func showVisitorRoom(code: String, room: Room) {
        guard let currentAccessToken else { return }
        let viewModel = RoomEntryViewModel(
            room: room,
            viewerRole: .visitor,
            design: variantLayouts,
            textures: RoomPhotoTextureLoader(
                photoService: SharedRoomPhotoTickets(shareLinkService: shareLinkClient, code: code),
                downloader: URLSessionPhotoDownloader()
            ),
            accessToken: currentAccessToken,
            sculptureModels: UnavailableSculptureModelProvider(),
            musicCatalog: museumClient,
            musicPlayer: musicPlayer,
            bundleRetention: activeBundles
        )
        let viewController = RoomEntryViewController(
            viewModel: viewModel,
            onCancel: { [weak self] in self?.navigationController.popViewController(animated: true) }
        ) { [weak self] content in
            guard let self else { return }
            let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime(diagnostics: diagnostics)
            var stack = navigationController.viewControllers
            stack.removeLast()
            stack.append(runtime.makeRoomViewController(content: content))
            navigationController.setViewControllers(stack, animated: true)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    private func leaveVisit() {
        showMainProductChoice()
    }

    private func showPrivacySettings() {
        guard let currentAccessToken else { return }
        let viewModel = PrivacySettingsViewModel(museumService: museumClient, accessToken: currentAccessToken)
        navigationController.pushViewController(
            PrivacySettingsViewController(viewModel: viewModel),
            animated: true
        )
    }

    private func showMuseumCreation() {
        let viewController = MuseumCreationFramingViewController { [weak self] in
            self?.showStyleSelection(context: .creatingMuseum)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    private func showStyleSelection(context: StyleSelectionViewModel.Context) {
        guard let currentAccessToken else { return }
        let viewModel = StyleSelectionViewModel(
            context: context,
            museumService: museumClient,
            catalogService: museumClient,
            accessToken: currentAccessToken,
            analytics: analytics
        )

        var selectionController: StyleSelectionViewController?
        let controller = StyleSelectionViewController(
            viewModel: viewModel,
            onPreviewStyle: { [weak self] style in
                self?.showStylePreview(
                    style: style,
                    viewModel: viewModel,
                    onChoose: { styleID in selectionController?.applyStyle(styleID) }
                )
            },
            onApplied: { [weak self] _ in
                self?.popToMuseumEntry()
            }
        )
        selectionController = controller
        navigationController.pushViewController(controller, animated: true)
    }

    private func showStylePreview(
        style: MuseumStyle,
        viewModel: StyleSelectionViewModel,
        onChoose: @escaping (String) -> Void
    ) {
        showPreview(
            subject: style.previewSubject,
            isCurrentlySelected: viewModel.isCurrentlySelected(style),
            confirmationReassurance: viewModel.confirmationReassurance,
            backButtonTitle: "Back to Styles",
            onChoose: onChoose
        )
    }

    private func showPreview(
        subject: PreviewSubject,
        isCurrentlySelected: Bool,
        confirmationReassurance: String?,
        backButtonTitle: String,
        onChoose: @escaping (String) -> Void
    ) {
        let previewViewModel = PreviewViewModel(
            subject: subject,
            isCurrentlySelected: isCurrentlySelected,
            confirmationReassurance: confirmationReassurance,
            assetProvider: previewAssets
        )

        let previewController = PreviewViewController(
            viewModel: previewViewModel,
            backButtonTitle: backButtonTitle,
            onChoose: { [weak self] id in
                self?.navigationController.dismiss(animated: true) { onChoose(id) }
            },
            onBack: { [weak self] in
                self?.navigationController.dismiss(animated: true)
            }
        )
        navigationController.present(previewController, animated: true)
    }

    private func popToMuseumEntry() {
        if let entry = navigationController.viewControllers.first(where: { $0 is MuseumEntryViewController }) {
            navigationController.popToViewController(entry, animated: true)
        } else {
            navigationController.popViewController(animated: true)
        }
    }

    // MARK: - Rooms

    private func showRoomList(museum: Museum) {
        guard let currentAccessToken else { return }
        refreshOwnedContentPins(accessToken: currentAccessToken)
        let viewModel = RoomListViewModel(museumService: museumClient, accessToken: currentAccessToken)
        let viewController = RoomListViewController(
            viewModel: viewModel,
            onCreateRoom: { [weak self] in self?.showRoomCreation(museum: museum) },
            onSelectRoom: { [weak self] room in
                self?.showRoomVariantSelection(
                    context: .changingVariant(room: room),
                    styleID: museum.styleID
                )
            },
            onAddPhotos: { [weak self] room in self?.showPhotoSelection(room: room) },
            onEnterRoom: { [weak self] room in self?.showRoomEntry(room: room) },
            onEnterLobby: { [weak self] in self?.showLobbyEntry() },
            onOpenRuntimeSkeleton: { [weak self] in self?.showRuntimeSkeleton() }
        )
        navigationController.pushViewController(viewController, animated: true)
    }

    // MARK: - Museum Lobby

    private func showLobbyEntry() {
        guard let currentAccessToken else { return }
        let viewModel = LobbyEntryViewModel(
            viewerRole: .owner,
            contentSource: OwnedMuseumLobbyContent(museumService: museumClient),
            geometry: UnavailableLobbyGeometryProvider(),
            cardTables: UnavailableLobbyCardTableProvider(),
            accessToken: currentAccessToken
        )
        let viewController = LobbyEntryViewController(
            viewModel: viewModel,
            onEnterLobby: { [weak self] content in
                guard let self else { return }
                let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime(diagnostics: diagnostics)
                let lobby = runtime.makeLobbyViewController(content: content) { [weak self] roomID in
                    self?.enterRoomFromLobby(roomID: roomID)
                }
                replaceTopViewController(with: lobby)
            },
            onEnterRoomDirectly: { [weak self] room in
                guard let self else { return }
                var stack = navigationController.viewControllers
                stack.removeLast()
                navigationController.setViewControllers(stack, animated: false)
                showRoomEntry(room: room)
            },
            onManageRooms: { [weak self] in self?.popToRoomList() }
        )
        navigationController.pushViewController(viewController, animated: true)
    }

    private func enterRoomFromLobby(roomID: String) {
        guard let currentAccessToken else { return }
        Task { [weak self] in
            guard let self else { return }
            do {
                let room = try await museumClient.fetchRoom(accessToken: currentAccessToken, roomID: roomID)
                showRoomEntry(room: room)
            } catch {
                let alert = UIAlertController(
                    title: "Couldn't open that Room",
                    message: "Please try again.",
                    preferredStyle: .alert
                )
                alert.addAction(UIAlertAction(title: "OK", style: .default))
                navigationController.topViewController?.present(alert, animated: true)
            }
        }
    }

    private func replaceTopViewController(with viewController: UIViewController) {
        var stack = navigationController.viewControllers
        stack.removeLast()
        stack.append(viewController)
        navigationController.setViewControllers(stack, animated: true)
    }

    // MARK: - Room Entry

    private func showRoomEntry(room: Room) {
        guard let currentAccessToken else { return }
        let viewModel = RoomEntryViewModel(
            room: room,
            design: variantLayouts,
            textures: RoomPhotoTextureLoader(
                photoService: photoAPIClient,
                downloader: URLSessionPhotoDownloader()
            ),
            accessToken: currentAccessToken,
            photoService: photoAPIClient,
            roomService: museumClient,
            photoReplacer: photoUploader,
            sculptureModels: UnavailableSculptureModelProvider(),
            catalogService: museumClient,
            musicCatalog: museumClient,
            musicPlayer: musicPlayer,
            bundleRetention: activeBundles
        )
        let viewController = RoomEntryViewController(
            viewModel: viewModel,
            onCancel: { [weak self] in self?.navigationController.popViewController(animated: true) }
        ) { [weak self] content in
            guard let self else { return }
            let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime(diagnostics: diagnostics)
            var stack = navigationController.viewControllers
            stack.removeLast()
            let roomViewController = runtime.makeRoomViewController(content: content)
            (roomViewController as? RealityKitSceneViewController)?.onAssignMusic = { [weak self] content in
                self?.showRoomMusicSelection(room: content.room)
            }
            stack.append(roomViewController)
            navigationController.setViewControllers(stack, animated: true)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    // MARK: - Room Music

    private func showRoomMusicSelection(room: Room) {
        guard let currentAccessToken else { return }
        let viewModel = RoomMusicSelectionViewModel(
            assignedTrackID: room.musicTrackID,
            assignment: MuseumRoomMusicAssignment(museumService: museumClient, accessToken: currentAccessToken, roomID: room.id),
            musicCatalog: museumClient,
            accessToken: currentAccessToken
        )
        let controller = RoomMusicSelectionViewController(viewModel: viewModel) { _ in }
        navigationController.pushViewController(controller, animated: true)
    }

    // MARK: - Collection Room Music

    private func showCollectionRoomMusic(room: CollectionRoom) {
        guard let currentAccessToken else { return }
        let viewModel = RoomMusicSelectionViewModel(
            assignedTrackID: room.musicTrackID,
            assignment: CollectionRoomMusicAssignment(
                service: collectionClient, accessToken: currentAccessToken, collectionRoomID: room.id
            ),
            musicCatalog: museumClient,
            accessToken: currentAccessToken
        )
        let session = RoomMusicSession(
            trackID: room.musicTrackID,
            catalog: museumClient,
            player: musicPlayer,
            accessToken: currentAccessToken
        )
        let controller = RoomMusicSelectionViewController(viewModel: viewModel, musicSession: session) { _ in
        }
        navigationController.pushViewController(controller, animated: true)
    }

    // MARK: - Photo Selection

    private func showPhotoSelection(room: Room) {
        guard let currentAccessToken else { return }
        let viewController = PhotoSelectionViewController(
            viewModel: PhotoSelectionViewModel(room: room, uploader: photoUploader, accessToken: currentAccessToken),
            onDone: { [weak self] in self?.popToRoomList() }
        )
        navigationController.pushViewController(viewController, animated: true)
    }

    private func showRoomCreation(museum: Museum) {
        let viewController = RoomCreationViewController(viewModel: RoomCreationViewModel()) { [weak self] name in
            self?.showRoomVariantSelection(context: .creatingRoom(name: name), styleID: museum.styleID)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    // MARK: - Room Variant

    private func showRoomVariantSelection(
        context: RoomVariantSelectionViewModel.Context,
        styleID: String
    ) {
        guard let currentAccessToken else { return }
        let viewModel = RoomVariantSelectionViewModel(
            context: context,
            museumService: museumClient,
            catalogService: museumClient,
            accessToken: currentAccessToken,
            styleID: styleID,
            analytics: analytics
        )

        var selectionController: RoomVariantSelectionViewController?
        let controller = RoomVariantSelectionViewController(
            viewModel: viewModel,
            onPreviewVariant: { [weak self] variant in
                self?.showPreview(
                    subject: variant.previewSubject,
                    isCurrentlySelected: viewModel.isCurrentlySelected(variant),
                    confirmationReassurance: viewModel.confirmationReassurance,
                    backButtonTitle: "Back to Designs",
                    onChoose: { variantID in selectionController?.applyVariant(variantID) }
                )
            },
            onApplied: { [weak self] _ in
                self?.popToRoomList()
            }
        )
        selectionController = controller
        navigationController.pushViewController(controller, animated: true)
    }

    private func popToRoomList() {
        if let list = navigationController.viewControllers.first(where: { $0 is RoomListViewController }) {
            navigationController.popToViewController(list, animated: true)
        } else {
            navigationController.popViewController(animated: true)
        }
    }

    private func showRuntimeSkeleton() {
        let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime(diagnostics: diagnostics)
        navigationController.pushViewController(runtime.makeRuntimeViewController(), animated: true)
    }

    private func showOwnProfile() {
        guard let currentAccessToken else { return }
        let viewModel = ProfileViewModel(profileService: apiClient, accessToken: currentAccessToken)
        let viewController = ProfileViewController(viewModel: viewModel) { [weak self] currentAvatarID in
            self?.showAvatarChanging(currentAvatarID: currentAvatarID)
        }
        navigationController.pushViewController(viewController, animated: true)
    }

    private func showAvatarChanging(currentAvatarID: String?) {
        guard let currentAccessToken else { return }
        let viewModel = AvatarSelectionViewModel(profileService: apiClient, accessToken: currentAccessToken)
        let viewController = AvatarSelectionViewController(viewModel: viewModel, currentAvatarID: currentAvatarID) { [weak self] _ in
            self?.navigationController.popViewController(animated: true)
        }
        navigationController.pushViewController(viewController, animated: true)
    }
}

// MARK: - Test seams

extension AppCoordinator {
    var testPendingShareLinkCode: String? { pendingShareLink?.code }
    var testPendingShareLink: MuseShareLink? { pendingShareLink }
    var testViewControllers: [UIViewController] { navigationController.viewControllers }

    func testCompleteLaunchRouting(accessToken: String?) {
        window.rootViewController = navigationController
        window.makeKeyAndVisible()
        currentAccessToken = accessToken
        if accessToken == nil {
            landAfterLaunch { showFirstLaunch() }
        } else {
            landAfterLaunch { showMainProductChoice() }
        }
    }

    func testStartAuthenticationFromLanding() { showAuthentication() }

    func testCompleteAuthentication(_ result: LoginResult) { routeAuthenticated(result) }

    func testCompleteAvatarOnboarding() { landAfterOnboarding() }

    func testDismissPendingShareLink() { dismissPendingShareLink() }

    func testContinueVisitorEntry(code: String, avatarID: String) {
        continueVisitorEntry(code: code, avatarID: avatarID)
    }

    func testContinueCollectionVisitorEntry(code: String, content: SharedCollectionRoomContent, avatarID: String) {
        continueCollectionVisitorEntry(code: code, content: content, avatarID: avatarID)
    }
}

final class AccessTokenBox: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: String?

    var value: String? {
        get {
            lock.lock()
            defer { lock.unlock() }
            return stored
        }
        set {
            lock.lock()
            stored = newValue
            lock.unlock()
        }
    }
}
