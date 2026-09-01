import XCTest
@testable import MuseApp

final class ShareLinkURLTests: XCTestCase {
    private let hosts: Set<String> = ["muse.app", "localhost"]
    private let code = "abcdefghijklmnopqrstuv"

    func test_acceptsAMuseShareLink() {
        XCTAssertEqual(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/\(code)")!, acceptedHosts: hosts), code)
        XCTAssertEqual(MuseShareLinkURL.code(from: URL(string: "HTTPS://MUSE.APP/m/\(code)")!, acceptedHosts: hosts), code,
                       "scheme and host are case-insensitive")
        XCTAssertEqual(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/\(code)?utm=x#frag")!, acceptedHosts: hosts), code,
                       "query and fragment do not change the code")
    }

    func test_rejectsALookAlikeHost() {
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app.evil.example/m/\(code)")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://notmuse.app/m/\(code)")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://evil.example/m/\(code)")!, acceptedHosts: ["muse.app"]))
    }

    func test_requiresHTTPS_exceptForLoopbackDevelopment() {
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "http://muse.app/m/\(code)")!, acceptedHosts: hosts))
        XCTAssertEqual(MuseShareLinkURL.code(from: URL(string: "http://localhost:8080/m/\(code)")!, acceptedHosts: hosts), code)
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "muse://m/\(code)")!, acceptedHosts: hosts),
                     "no custom scheme — the link is a Universal Link or nothing")
    }

    func test_requiresExactlyThePathShape() {
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/\(code)")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/\(code)/rooms/r1")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/museums/\(code)")!, acceptedHosts: hosts))
    }

    func test_requiresAPlausibleCode() {
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/short")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/\(code)X")!, acceptedHosts: hosts))
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/abcdefghijklmnopqrstu%3D")!, acceptedHosts: hosts))
        XCTAssertTrue(MuseShareLinkURL.isPlausibleCode("0123456789-_ABCDEFGHIJ"))
        XCTAssertFalse(MuseShareLinkURL.isPlausibleCode("0123456789-_ABCDEFGHI="))
    }
}

final class DeepLinkRoutingTests: XCTestCase {
    func test_afterAuthentication_returningUserWithPendingLink_goesToTheLanding() {
        XCTAssertEqual(
            DeepLinkRouting.destinationAfterAuthentication(pendingShareLinkCode: "c", isNewAccount: false),
            .sharedMuseumLanding(code: "c")
        )
    }

    func test_afterAuthentication_withoutPendingLink_goesHome() {
        XCTAssertEqual(DeepLinkRouting.destinationAfterAuthentication(pendingShareLinkCode: nil, isNewAccount: false), .mainHub)
    }

    func test_afterAuthentication_newAccount_onboardsFirst_thenHonoursTheLink() {
        XCTAssertEqual(DeepLinkRouting.destinationAfterAuthentication(pendingShareLinkCode: "c", isNewAccount: true), .accountCreation)
        XCTAssertEqual(DeepLinkRouting.destinationAfterOnboarding(pendingShareLinkCode: "c"), .sharedMuseumLanding(code: "c"))
        XCTAssertEqual(DeepLinkRouting.destinationAfterOnboarding(pendingShareLinkCode: nil), .mainHub)
    }

    func test_ownerOpeningTheirOwnLink_landsAsOwner() {
        XCTAssertEqual(DeepLinkRouting.sharedMuseumEntry(sharedMuseumID: "m1", ownMuseumID: "m1"), .ownMuseum)
        XCTAssertEqual(DeepLinkRouting.sharedMuseumEntry(sharedMuseumID: "m1", ownMuseumID: "m2"), .visitor)
        XCTAssertEqual(DeepLinkRouting.sharedMuseumEntry(sharedMuseumID: "m1", ownMuseumID: nil), .visitor,
                       "a visitor with no Museum of their own is a visitor")
    }
}

final class CollectionShareLinkURLTests: XCTestCase {
    private let hosts: Set<String> = ["muse.app", "localhost"]
    private let code = "abcdefghijklmnopqrstuv"

    func test_parse_tellsTheTwoKindsApart_byPath() {
        XCTAssertEqual(MuseShareLinkURL.parse(URL(string: "https://muse.app/c/\(code)")!, acceptedHosts: hosts), .collectionRoom(code: code))
        XCTAssertEqual(MuseShareLinkURL.parse(URL(string: "https://muse.app/m/\(code)")!, acceptedHosts: hosts), .museum(code: code))
        XCTAssertEqual(MuseShareLinkURL.parse(URL(string: "http://localhost:8080/c/\(code)")!, acceptedHosts: hosts), .collectionRoom(code: code))
        XCTAssertEqual(MuseShareLink.collectionRoom(code: code).code, code)
    }

    func test_museumCodeExtraction_ignoresACollectionLink() {
        XCTAssertNil(MuseShareLinkURL.code(from: URL(string: "https://muse.app/c/\(code)")!, acceptedHosts: hosts))
        XCTAssertEqual(MuseShareLinkURL.code(from: URL(string: "https://muse.app/m/\(code)")!, acceptedHosts: hosts), code)
    }

    func test_parse_isAsStrictAsTheMuseumParser() {
        XCTAssertNil(MuseShareLinkURL.parse(URL(string: "https://muse.app/x/\(code)")!, acceptedHosts: hosts), "unknown path")
        XCTAssertNil(MuseShareLinkURL.parse(URL(string: "https://muse.app.evil.example/c/\(code)")!, acceptedHosts: hosts), "look-alike host")
        XCTAssertNil(MuseShareLinkURL.parse(URL(string: "http://muse.app/c/\(code)")!, acceptedHosts: hosts), "HTTP off loopback")
        XCTAssertNil(MuseShareLinkURL.parse(URL(string: "https://muse.app/c/short")!, acceptedHosts: hosts), "implausible code")
        XCTAssertNil(MuseShareLinkURL.parse(URL(string: "https://muse.app/c/\(code)/items")!, acceptedHosts: hosts), "extra segment")
        XCTAssertNil(MuseShareLinkURL.parse(URL(string: "https://muse.app/collection-rooms/\(code)")!, acceptedHosts: hosts), "a Room id is not a link")
    }
}

final class CollectionDeepLinkRoutingTests: XCTestCase {
    func test_aPendingCollectionLink_routesToTheCollectionLanding() {
        XCTAssertEqual(
            DeepLinkRouting.destinationAfterAuthentication(pendingShareLink: .collectionRoom(code: "c"), isNewAccount: false),
            .sharedCollectionRoomLanding(code: "c")
        )
        XCTAssertEqual(
            DeepLinkRouting.destinationAfterOnboarding(pendingShareLink: .collectionRoom(code: "c")),
            .sharedCollectionRoomLanding(code: "c")
        )
        XCTAssertEqual(
            DeepLinkRouting.destinationAfterOnboarding(pendingShareLink: .museum(code: "m")),
            .sharedMuseumLanding(code: "m")
        )
        XCTAssertEqual(DeepLinkRouting.destinationAfterOnboarding(pendingShareLink: nil), .mainHub)
    }

    func test_aNewAccount_onboardsFirst_whateverTheLinkKind() {
        XCTAssertEqual(
            DeepLinkRouting.destinationAfterAuthentication(pendingShareLink: .collectionRoom(code: "c"), isNewAccount: true),
            .accountCreation
        )
    }

    func test_stringOverloads_stillMeanAMuseumLink() {
        XCTAssertEqual(DeepLinkRouting.destinationAfterOnboarding(pendingShareLinkCode: "m"), .sharedMuseumLanding(code: "m"))
    }
}
