import Foundation
@testable import MuseApp

final class ScriptedDesignProvider: RoomDesignProviding, @unchecked Sendable {
    private let lock = NSLock()
    private var script: [RoomDesignLoadState]
    private var resolution: RoomDesignResolution
    private var gate: CheckedContinuation<Void, Never>?
    private var shouldSuspend: Bool
    private(set) var callCount = 0
    private(set) var observedCancellation = false

    init(
        progress: [RoomDesignLoadState] = [],
        resolution: RoomDesignResolution,
        suspendUntilReleased: Bool = false
    ) {
        self.script = progress
        self.resolution = resolution
        self.shouldSuspend = suspendUntilReleased
    }

    static func fixture(progress: [RoomDesignLoadState] = []) -> ScriptedDesignProvider {
        ScriptedDesignProvider(
            progress: progress,
            resolution: .available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture))
        )
    }

    static func unavailable(_ reason: RoomDesignUnavailableReason) -> ScriptedDesignProvider {
        ScriptedDesignProvider(resolution: .unavailable(reason))
    }

    func answer(_ resolution: RoomDesignResolution, progress: [RoomDesignLoadState] = []) {
        lock.lock()
        self.resolution = resolution
        self.script = progress
        lock.unlock()
    }

    func release() {
        lock.lock()
        let gate = self.gate
        self.gate = nil
        shouldSuspend = false
        lock.unlock()
        gate?.resume()
    }

    func design(
        forVariantID variantID: String,
        progress: @escaping @Sendable (RoomDesignLoadState) -> Void
    ) async -> RoomDesignResolution {
        let call = beginCall()

        for state in call.script {
            progress(state)
            await Task.yield()
        }

        if call.suspend {
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                park(continuation)
            }
            if Task.isCancelled {
                markCancelled()
            }
        }
        return call.resolution
    }

    private struct Call {
        let script: [RoomDesignLoadState]
        let resolution: RoomDesignResolution
        let suspend: Bool
    }

    private func beginCall() -> Call {
        lock.lock()
        defer { lock.unlock() }
        callCount += 1
        return Call(script: script, resolution: resolution, suspend: shouldSuspend)
    }

    private func park(_ continuation: CheckedContinuation<Void, Never>) {
        lock.lock()
        defer { lock.unlock() }
        gate = continuation
    }

    private func markCancelled() {
        lock.lock()
        defer { lock.unlock() }
        observedCancellation = true
    }
}
