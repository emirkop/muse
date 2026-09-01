import XCTest
@testable import MuseApp

final class APIDateDecodingTests: XCTestCase {
    private let whole = RFC3339Date.parse("2026-08-26T22:24:58Z")!

    func test_parsesAWholeSecondTimestamp() throws {
        let reference = ISO8601DateFormatter()
        reference.formatOptions = [.withInternetDateTime]
        XCTAssertEqual(whole, try XCTUnwrap(reference.date(from: "2026-08-26T22:24:58Z")))
    }

    func test_parsesMilliseconds() throws {
        let parsed = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58.454Z"))
        XCTAssertEqual(parsed.timeIntervalSince(whole), 0.454, accuracy: 1e-6)
    }

    func test_parsesMicrosecondsAndNanoseconds() throws {
        let micro = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58.454662Z"))
        XCTAssertEqual(micro.timeIntervalSince(whole), 0.454662, accuracy: 1e-6)
        let nano = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58.454662123Z"))
        XCTAssertEqual(nano.timeIntervalSince(whole), 0.454662123, accuracy: 1e-6)
        let tenth = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58.5Z"))
        XCTAssertEqual(tenth.timeIntervalSince(whole), 0.5, accuracy: 1e-6)
    }

    func test_parsesTheLiveDevServerShape() throws {
        let live = try XCTUnwrap(RFC3339Date.parse("2026-08-26T15:45:41.052175+03:00"))
        let sameInstantUTC = try XCTUnwrap(RFC3339Date.parse("2026-08-26T12:45:41Z"))
        XCTAssertEqual(live.timeIntervalSince(sameInstantUTC), 0.052175, accuracy: 1e-6)
    }

    func test_parsesAnOffsetZone() throws {
        let offset = try XCTUnwrap(RFC3339Date.parse("2026-08-27T00:24:58.25+02:00"))
        XCTAssertEqual(offset.timeIntervalSince(whole), 0.25, accuracy: 1e-6)
        let negative = try XCTUnwrap(RFC3339Date.parse("2026-08-26T17:24:58-05:00"))
        XCTAssertEqual(negative, whole)
    }

    func test_malformedInputIsRefused() {
        for malformed in [
            "",
            "not-a-date",
            "1756247098",
            "2026-08-26 22:24:58Z",
            "2026-08-26T22:24:58",
            "2026-08-26T22:24:58.Z",
            "2026-08-26T22:24:58.12a4Z",
            "2026-08-26T22:24:58.1234567890Z",
            "2026-13-40T22:24:58Z",
            "2026-08-26T22:24Z",
            "2026-08-26T22:24:58.454Z.",
        ] {
            XCTAssertNil(RFC3339Date.parse(malformed), malformed.debugDescription)
        }
    }

    func test_decodingStrategy_acceptsEveryShape_andThrowsOnMalformed() throws {
        struct Body: Decodable { let at: Date }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .rfc3339

        for (json, offset) in [
            (#"{"at":"2026-08-26T22:24:58Z"}"#, 0.0),
            (#"{"at":"2026-08-26T22:24:58.454Z"}"#, 0.454),
            (#"{"at":"2026-08-26T22:24:58.454662Z"}"#, 0.454662),
        ] {
            let body = try decoder.decode(Body.self, from: Data(json.utf8))
            XCTAssertEqual(body.at.timeIntervalSince(whole), offset, accuracy: 1e-6, json)
        }

        XCTAssertThrowsError(try decoder.decode(Body.self, from: Data(#"{"at":"garbage"}"#.utf8))) { error in
            guard case DecodingError.dataCorrupted = error else {
                return XCTFail("expected dataCorrupted, got \(error)")
            }
        }
        XCTAssertThrowsError(try decoder.decode(Body.self, from: Data(#"{"at":1756247098}"#.utf8)),
                             "a number is not a timestamp on this API")
    }
}
