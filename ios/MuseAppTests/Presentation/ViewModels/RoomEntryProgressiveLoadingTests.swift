import XCTest
@testable import MuseApp

@MainActor
final class RoomEntryProgressiveLoadingTests: XCTestCase {

    private func room() -> Room {
        Room(id: "r", name: "Study", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
             photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a0", caption: "")])
    }

    private func statesDuringLoad(_ viewModel: RoomEntryViewModel) async -> [RoomEntryViewModel.State] {
        let recorder = StateSequenceRecorder()
        viewModel.onStateChange = { recorder.record($0) }
        await viewModel.load()
        await Task.yield()
        return recorder.states
    }

    // MARK: Geometry-first progressive reveal

    func test_geometryReady_isReportedBeforeReady_andFractionsNeverRegress() async {
        let provider = ScriptedDesignProvider.fixture(progress: [
            .checking,
            .downloading(fractionComplete: 0.10),
            .downloading(fractionComplete: 0.45),
            .geometryReady(fractionComplete: 0.60),
            .downloading(fractionComplete: 0.30),
            .geometryReady(fractionComplete: 0.95),
            .ready
        ])
        let viewModel = RoomEntryViewModel(room: room(), design: provider, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .seconds(60))

        let states = await statesDuringLoad(viewModel)

        XCTAssertEqual(states.last, .ready)
        let geometryIndex = states.firstIndex { if case .geometryReady = $0 { return true } else { return false } }
        XCTAssertNotNil(geometryIndex, "geometry must be reported as ready before the whole design is")
        XCTAssertLessThan(geometryIndex!, states.count - 1)

        var lastFraction = 0.0
        var sawGeometry = false
        for state in states {
            switch state {
            case .downloading(let f):
                XCTAssertFalse(sawGeometry, "downloading after geometryReady would be a regression: \(states)")
                XCTAssertGreaterThanOrEqual(f, lastFraction)
                lastFraction = f
            case .geometryReady(let f):
                sawGeometry = true
                XCTAssertGreaterThanOrEqual(f, lastFraction)
                lastFraction = f
            default: break
            }
        }
        XCTAssertNotNil(viewModel.content)
    }

    // MARK: Progress state

    func test_downloadingFraction_isForwardedForADeterminateIndicator() async {
        let provider = ScriptedDesignProvider.fixture(progress: [.downloading(fractionComplete: 0.25), .downloading(fractionComplete: 0.75)])
        let viewModel = RoomEntryViewModel(room: room(), design: provider, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .seconds(60))

        let states = await statesDuringLoad(viewModel)

        XCTAssertTrue(states.contains(.downloading(fractionComplete: 0.25)))
        XCTAssertTrue(states.contains(.downloading(fractionComplete: 0.75)))
        XCTAssertEqual(viewModel.loadingCopy(for: .downloading(fractionComplete: 0.75)).detail, "75% · Later visits will be near-instant.")
    }

    func test_checkingPastTheThreshold_showsExtendedWaitCopy() async {
        let provider = ScriptedDesignProvider.fixture(progress: [], )
        provider.answer(.available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture)))
        let suspended = ScriptedDesignProvider(
            resolution: .available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture)),
            suspendUntilReleased: true
        )
        let viewModel = RoomEntryViewModel(room: room(), design: suspended, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .zero)
        XCTAssertFalse(viewModel.showsExtendedWaitCopy)

        let load = Task { await viewModel.load() }
        for _ in 0..<20 where !viewModel.showsExtendedWaitCopy { await Task.yield() }

        XCTAssertEqual(viewModel.state, .checking)
        XCTAssertTrue(viewModel.showsExtendedWaitCopy, "past the threshold the screen must say it is still working")
        XCTAssertEqual(viewModel.loadingCopy(for: .checking).title, "Still preparing your Room")

        suspended.release()
        await load.value
        XCTAssertEqual(viewModel.state, .ready)
        _ = provider
    }

    func test_extendedWaitCopy_doesNotAppear_whenTheLoadFinishesFirst() async {
        let viewModel = RoomEntryViewModel(room: room(), design: ScriptedDesignProvider.fixture(), textures: NoTextures(),
                                           accessToken: "t", extendedWaitThreshold: .seconds(60))
        await viewModel.load()
        XCTAssertFalse(viewModel.showsExtendedWaitCopy)
        XCTAssertEqual(viewModel.loadingCopy(for: .checking).title, "Preparing your Room")
    }

    // MARK: Cache hit path

    func test_cacheHit_goesStraightFromCheckingToReady() async {
        let provider = ScriptedDesignProvider.fixture(progress: [.checking, .ready])
        let viewModel = RoomEntryViewModel(room: room(), design: provider, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .seconds(60))

        let states = await statesDuringLoad(viewModel)

        XCTAssertEqual(states, [.ready], "from the initial .checking straight to .ready — no download state in between")
        XCTAssertFalse(viewModel.showsExtendedWaitCopy)
    }

    // MARK: Failure / retry

    func test_everyFailureReason_isAnExplicitState_withCopy() async {
        let reasons: [RoomDesignUnavailableReason] = [
            .notPublished, .deliveryUnconfigured, .variantUnknown, .offline, .network,
            .corruptDownload, .storage, .malformedBundle, .layoutMismatch
        ]
        for reason in reasons {
            let viewModel = RoomEntryViewModel(room: room(), design: ScriptedDesignProvider.unavailable(reason),
                                               textures: NoTextures(), accessToken: "t")
            await viewModel.load()
            XCTAssertEqual(viewModel.state, .designUnavailable(variantID: PlaceholderRoomSlotTable.variantID, reason: reason))
            XCTAssertNil(viewModel.content)
            let copy = viewModel.designUnavailableCopy(reason)
            XCTAssertFalse(copy.title.isEmpty, "\(reason) needs a title")
            XCTAssertFalse(copy.message.isEmpty, "\(reason) needs a message")
        }
        XCTAssertFalse(RoomEntryViewModel(room: room(), design: UnavailableRoomDesignProvider(), textures: NoTextures(), accessToken: "t")
            .designUnavailableCopy(.malformedBundle).canRetry)
        XCTAssertTrue(RoomEntryViewModel(room: room(), design: UnavailableRoomDesignProvider(), textures: NoTextures(), accessToken: "t")
            .designUnavailableCopy(.network).canRetry)
    }

    func test_offlineReason_hasItsOwnActionableCopy() {
        let viewModel = RoomEntryViewModel(room: room(), design: UnavailableRoomDesignProvider(),
                                           textures: NoTextures(), accessToken: "t")
        let offline = viewModel.designUnavailableCopy(.offline)
        XCTAssertTrue(offline.title.lowercased().contains("offline"), offline.title)
        XCTAssertNotEqual(offline.message, viewModel.designUnavailableCopy(.network).message)
        XCTAssertTrue(offline.canRetry)
    }

    func test_aFailedDownload_thenRetry_resolvesFresh() async {
        let provider = ScriptedDesignProvider(progress: [.downloading(fractionComplete: 0.4)], resolution: .unavailable(.network))
        let viewModel = RoomEntryViewModel(room: room(), design: provider, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .seconds(60))

        await viewModel.load()
        XCTAssertEqual(viewModel.state, .designUnavailable(variantID: PlaceholderRoomSlotTable.variantID, reason: .network),
                       "a failed download is a terminal, explicit state — never a spinner left running")

        provider.answer(.available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture)),
                        progress: [.downloading(fractionComplete: 0.4), .downloading(fractionComplete: 1.0), .ready])
        await viewModel.load()
        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertEqual(provider.callCount, 2)
    }

    func test_progressFromASupersededLoad_isIgnored() async {
        let slow = ScriptedDesignProvider(
            progress: [.downloading(fractionComplete: 0.5)],
            resolution: .unavailable(.network),
            suspendUntilReleased: true
        )
        let viewModel = RoomEntryViewModel(room: room(), design: slow, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .seconds(60))

        let first = Task { await viewModel.load() }
        for _ in 0..<20 where viewModel.state == .checking { await Task.yield() }

        slow.answer(.available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture)))
        first.cancel()
        slow.release()
        await first.value
        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready, "the newer load owns the screen; the first attempt's outcome is dropped")
    }

    // MARK: Cancellation

    func test_cancellingTheLoad_leavesTheStateNonTerminal_andTheProviderSeesIt() async {
        let suspended = ScriptedDesignProvider(resolution: .unavailable(.network), suspendUntilReleased: true)
        let viewModel = RoomEntryViewModel(room: room(), design: suspended, textures: NoTextures(), accessToken: "t",
                                           extendedWaitThreshold: .seconds(60))

        let load = Task { await viewModel.load() }
        for _ in 0..<20 where suspended.callCount == 0 { await Task.yield() }
        load.cancel()
        suspended.release()
        await load.value

        XCTAssertEqual(viewModel.state, .checking, "a cancelled load neither succeeds nor reports a failure the user did not see")
        XCTAssertNil(viewModel.content)
        XCTAssertTrue(suspended.observedCancellation, "the provider must observe cancellation so a download stops asking for bytes")
    }
}

final class StateSequenceRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var collected: [RoomEntryViewModel.State] = []
    func record(_ state: RoomEntryViewModel.State) { lock.lock(); collected.append(state); lock.unlock() }
    var states: [RoomEntryViewModel.State] { lock.lock(); defer { lock.unlock() }; return collected }
}
