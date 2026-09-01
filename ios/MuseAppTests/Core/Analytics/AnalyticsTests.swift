import XCTest
@testable import MuseApp

final class AnalyticsTests: XCTestCase {

    // MARK: - The wire form

    func test_everyEventEncodesOnlyItsOwnTypedProperties() throws {
        let cases: [(AnalyticsEvent, String, Set<String>)] = [
            (.museumCreationStep(.stylePreviewed), "museum_creation_step",
             ["event_uuid", "name", "step"]),
            (.roomCreationStep(.nameEntered), "room_creation_step",
             ["event_uuid", "name", "step"]),
            (.collectionRoomCreationStep(.categoryChosen, categoryID: "category_watches"),
             "collection_room_creation_step", ["event_uuid", "name", "step", "category_id"]),
            (.catalogSearchOutcome(.selected, categoryID: "category_coins"),
             "catalog_search_outcome", ["event_uuid", "name", "outcome", "category_id"]),
            (.capacityUpgradeStep(.purchaseStarted), "capacity_upgrade_step",
             ["event_uuid", "name", "step"]),
            (.failureShown(surface: .roomEntry, classification: .offline, retried: true, retrySucceeded: false),
             "failure_shown", ["event_uuid", "name", "surface", "classification", "retried", "retry_succeeded"]),
        ]

        for (event, expectedName, expectedKeys) in cases {
            let data = try JSONEncoder().encode(event.payload(uuid: sampleUUID))
            let object = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
            XCTAssertEqual(object["name"] as? String, expectedName)
            XCTAssertEqual(Set(object.keys), expectedKeys,
                           "\(expectedName) encoded \(Set(object.keys).symmetricDifference(expectedKeys)) unexpectedly")
        }
    }

    func test_noEventCanCarryANameACaptionOrAQuery() throws {
        let forbidden = ["room_name", "name_text", "caption", "query", "search_text",
                         "email", "display_name", "share_code", "message", "error"]
        let events: [AnalyticsEvent] = [
            .museumCreationStep(.styleConfirmed),
            .roomCreationStep(.nameEntered),
            .collectionRoomCreationStep(.nameEntered, categoryID: nil),
            .catalogSearchOutcome(.abandoned, categoryID: "category_watches"),
            .capacityUpgradeStep(.purchaseFailed),
            .failureShown(surface: .catalogSearch, classification: .server, retried: true, retrySucceeded: true),
        ]
        for event in events {
            let data = try JSONEncoder().encode(event.payload(uuid: sampleUUID))
            let object = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
            for key in forbidden {
                XCTAssertNil(object[key], "\(event.name) carried \(key)")
            }
            for (key, value) in object where key != "event_uuid" {
                if let text = value as? String {
                    XCTAssertLessThanOrEqual(text.count, 64, "\(key) = \(text) is long enough to be free text")
                    XCTAssertNil(text.rangeOfCharacter(from: .whitespaces),
                                 "\(key) = \(text) contains whitespace, which no contract value does")
                }
            }
        }
    }

    func test_theClientCannotEmitServerOnlyEvents() {
        let clientNames: Set<String> = [
            AnalyticsEvent.museumCreationStep(.styleListShown).name,
            AnalyticsEvent.roomCreationStep(.nameEntered).name,
            AnalyticsEvent.collectionRoomCreationStep(.nameEntered, categoryID: nil).name,
            AnalyticsEvent.catalogSearchOutcome(.selected, categoryID: "c").name,
            AnalyticsEvent.capacityUpgradeStep(.purchaseStarted).name,
            AnalyticsEvent.failureShown(surface: .music, classification: .content, retried: false, retrySucceeded: false).name,
        ]
        XCTAssertFalse(clientNames.contains("catalog_search_performed"))
        XCTAssertFalse(clientNames.contains("item_add_refused"))
        XCTAssertFalse(clientNames.contains("onboarding_step_reached"))
    }

    func test_failureClassificationFollowsTheResilienceRules() {
        XCTAssertEqual(AnalyticsEvent.FailureClassification.of(URLError(.notConnectedToInternet)), .offline)
        XCTAssertEqual(AnalyticsEvent.FailureClassification.of(URLError(.timedOut)), .unreachable)
        XCTAssertEqual(
            AnalyticsEvent.FailureClassification.of(IdentityAPIClientError.server(statusCode: 403, message: nil)),
            .server)
    }

    // MARK: - Emission behaviour

    func test_eachEventGetsAFreshUUIDSoItIsNotAnIdentifier() async {
        let submitter = RecordingSubmitter()
        let recorder = AnalyticsRecorder(client: submitter, accessToken: { "token" })

        recorder.record(.museumCreationStep(.styleListShown))
        recorder.record(.museumCreationStep(.stylePreviewed))
        await submitter.wait(forBatches: 2)

        let uuids = await submitter.allUUIDs()
        XCTAssertEqual(uuids.count, 2)
        XCTAssertEqual(Set(uuids).count, 2,
                       "two events must not share an id — a stable id across events is a device identifier")
    }

    func test_recordingIsFireAndForgetEvenWhenSubmissionFails() async {
        let submitter = FailingSubmitter()
        let recorder = AnalyticsRecorder(client: submitter, accessToken: { "token" })

        recorder.record(.failureShown(surface: .roomList, classification: .offline,
                                      retried: false, retrySucceeded: false))
        await submitter.wait(forCalls: 1)
        let calls = await submitter.calls
        XCTAssertEqual(calls, 1)
    }

    func test_noPreAuthEventPathExists() async {
        let submitter = RecordingSubmitter()
        let recorder = AnalyticsRecorder(client: submitter, accessToken: { nil })

        recorder.record(.museumCreationStep(.styleListShown))
        try? await Task.sleep(nanoseconds: 100_000_000)

        let batches = await submitter.batchCount
        XCTAssertEqual(batches, 0,
                       "an event emitted with no session must be dropped, not queued and not attributed to a device")
    }

    func test_nothingIsPersistedForLaterDelivery() async {
        let submitter = FailingSubmitter()
        let recorder = AnalyticsRecorder(client: submitter, accessToken: { "token" })
        let defaultsBefore = UserDefaults.standard.dictionaryRepresentation().count

        recorder.record(.capacityUpgradeStep(.purchaseFailed))
        await submitter.wait(forCalls: 1)

        XCTAssertEqual(UserDefaults.standard.dictionaryRepresentation().count, defaultsBefore,
                       "analytics must not write to UserDefaults")
        let fresh = RecordingSubmitter()
        _ = AnalyticsRecorder(client: fresh, accessToken: { "token" })
        try? await Task.sleep(nanoseconds: 50_000_000)
        let inherited = await fresh.batchCount
        XCTAssertEqual(inherited, 0, "a new recorder must not replay anything")
    }

    func test_theBufferIsBoundedAndDropsRatherThanGrows() async {
        let submitter = RecordingSubmitter()
        let recorder = AnalyticsRecorder(client: submitter, accessToken: { "token" })
        for _ in 0..<200 {
            recorder.record(.museumCreationStep(.styleListShown))
        }
        try? await Task.sleep(nanoseconds: 300_000_000)
        let largest = await submitter.largestBatch()
        XCTAssertLessThanOrEqual(largest, 20, "a batch larger than the buffer limit means it grew unbounded")
    }

    // MARK: - No third-party dependency

    func test_noThirdPartyAnalyticsDependencyExists() throws {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()

        let sdkModules = [
            "FirebaseAnalytics", "Firebase", "Amplitude", "Mixpanel", "Segment",
            "Analytics", "AppsFlyerLib", "AdjustSdk", "Sentry", "Bugsnag",
            "PostHog", "Heap", "TelemetryDeck", "Countly", "MetricKit",
            "AdSupport", "AppTrackingTransparency",
        ]
        let identifierSymbols = [
            "ASIdentifierManager", "ATTrackingManager", "identifierForVendor",
            "advertisingIdentifier", "requestTrackingAuthorization",
        ]

        var scanned = 0
        for target in ["MuseApp", "project.yml"] {
            for file in filesUnder(root.appendingPathComponent(target)) {
                guard let source = try? String(contentsOf: file, encoding: .utf8) else { continue }
                scanned += 1
                for module in sdkModules {
                    XCTAssertFalse(source.contains("import \(module)"),
                                   "\(file.lastPathComponent) imports \(module); chose Muse-owned analytics with no SDK")
                    if file.pathExtension == "yml" {
                        XCTAssertFalse(source.contains(module),
                                       "project.yml references \(module) as a dependency")
                    }
                }
                for symbol in identifierSymbols {
                    XCTAssertFalse(source.contains(symbol),
                                   "\(file.lastPathComponent) uses \(symbol); forbids a device identifier")
                }
            }
        }
        XCTAssertGreaterThan(scanned, 50, "scanned only \(scanned) files — the check is not testing anything")
    }

    private func filesUnder(_ url: URL) -> [URL] {
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory) else { return [] }
        if !isDirectory.boolValue { return [url] }
        guard let enumerator = FileManager.default.enumerator(at: url, includingPropertiesForKeys: nil) else { return [] }
        return enumerator.compactMap { $0 as? URL }.filter {
            ["swift", "yml", "plist"].contains($0.pathExtension)
        }
    }

    private let sampleUUID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
}

// MARK: - Support

actor RecordingSubmitter: AnalyticsSubmitting {
    private var batches: [[AnalyticsEventPayload]] = []

    var batchCount: Int { batches.count }

    func submit(events: [AnalyticsEventPayload], accessToken: String) async {
        batches.append(events)
    }

    func allUUIDs() -> [String] { batches.flatMap { $0 }.map(\.eventUUID) }
    func largestBatch() -> Int { batches.map(\.count).max() ?? 0 }

    func wait(forBatches count: Int) async {
        for _ in 0..<200 where batches.count < count {
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
    }
}

actor FailingSubmitter: AnalyticsSubmitting {
    private(set) var calls = 0

    func submit(events: [AnalyticsEventPayload], accessToken: String) async {
        calls += 1
    }

    func wait(forCalls count: Int) async {
        for _ in 0..<200 where calls < count {
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
    }
}
