import XCTest
@testable import MuseApp

final class RoomPhotoCaptionsTests: XCTestCase {

    private func slots(_ captions: [String]) -> [PhotoSlotAssignment] {
        captions.enumerated().map { index, caption in
            PhotoSlotAssignment(slotIndex: index, photoAssetID: "a\(index)", caption: caption)
        }
    }

    // MARK: - Rules

    func test_normalised_trimsSurroundingWhitespaceOnly() {
        XCTAssertEqual(CaptionRules.normalised("  Trabzon, 1998  "), "Trabzon, 1998")
        XCTAssertEqual(CaptionRules.normalised("\n\tTrabzon\n"), "Trabzon")
        XCTAssertEqual(CaptionRules.normalised("my   grandmother's  house"), "my   grandmother's  house")
    }

    func test_whitespaceOnlyCaption_isNoCaption() {
        for blank in ["", " ", "   ", "\n", "\t \n"] {
            XCTAssertTrue(CaptionRules.isEmpty(blank), "\(blank.debugDescription) must count as no caption")
        }
        XCTAssertFalse(CaptionRules.isEmpty("."))
    }

    func test_counts_areCharactersForDisplayAndBytesForTheBound() {
        XCTAssertEqual(CaptionRules.characterCount("café"), 4)
        XCTAssertEqual(CaptionRules.byteCount("café"), 5)
        XCTAssertEqual(CaptionRules.characterCount("İstanbul"), 8)
        XCTAssertEqual(CaptionRules.byteCount("İstanbul"), 9)
        XCTAssertEqual(CaptionRules.characterCount("👨‍👩‍👧‍👦"), 1)
    }

    func test_rejection_onlyEverForLength_andOnlyAtTheBound() {
        XCTAssertNil(CaptionRules.rejection(for: ""))
        XCTAssertNil(CaptionRules.rejection(for: String(repeating: "a", count: CaptionRules.interimMaximumBytes)))
        XCTAssertEqual(
            CaptionRules.rejection(for: String(repeating: "a", count: CaptionRules.interimMaximumBytes + 1)),
            .tooLong(byteCount: CaptionRules.interimMaximumBytes + 1, limit: CaptionRules.interimMaximumBytes)
        )
    }

    func test_boundIsMeasuredAfterTrimming() {
        let atLimit = String(repeating: "a", count: CaptionRules.interimMaximumBytes)
        XCTAssertNil(CaptionRules.rejection(for: "   \(atLimit)   "))
    }

    func test_nothingIsFilteredRewrittenOrInterpreted() {
        let awkward = "**bold** <b>html</b> [link](url) damn 😤 \u{202E}rtl"
        XCTAssertEqual(CaptionRules.normalised(awkward), awkward, "no Markdown, no HTML, no profanity filter")
    }

    // MARK: - Mutation

    func test_setting_targetsThePhotographAndLeavesEverythingElseAlone() {
        let original = slots(["one", "two", "three"])
        let updated = RoomPhotoCaptions.setting("changed", forAssetID: "a1", in: original)

        XCTAssertEqual(updated.map(\.caption), ["one", "changed", "three"])
        XCTAssertEqual(updated.map(\.photoAssetID), original.map(\.photoAssetID), "identity untouched")
        XCTAssertEqual(updated.map(\.slotIndex), original.map(\.slotIndex), "order untouched")
    }

    func test_setting_normalisesAndTreatsBlankAsClearing() {
        let updated = RoomPhotoCaptions.setting("   ", forAssetID: "a0", in: slots(["was here"]))
        XCTAssertEqual(updated[0].caption, "")
        XCTAssertFalse(RoomPhotoCaptions.hasCaption(assetID: "a0", in: updated))
    }

    func test_setting_unknownPhotograph_changesNothing() {
        let original = slots(["one", "two"])
        XCTAssertEqual(RoomPhotoCaptions.setting("x", forAssetID: "ghost", in: original), original)
    }

    func test_caption_distinguishesNoCaptionFromNotInThisRoom() {
        let existing = slots(["", "two"])
        XCTAssertEqual(RoomPhotoCaptions.caption(forAssetID: "a0", in: existing), "", "in the Room, no caption")
        XCTAssertNil(RoomPhotoCaptions.caption(forAssetID: "ghost", in: existing), "not in the Room at all")
    }

    // MARK: - Captions survive reordering

    func test_captionsFollowTheirPhotographThroughASwap() {
        let original = slots(["zero", "one", "two", "three"])
        let swapped = RoomPhotoOrder.swapping(original, from: 0, to: 3)

        XCTAssertEqual(swapped.map(\.photoAssetID), ["a3", "a1", "a2", "a0"])
        XCTAssertEqual(swapped.map(\.caption), ["three", "one", "two", "zero"], "each caption travelled with its photograph")
        for slot in swapped {
            let expected = RoomPhotoCaptions.caption(forAssetID: slot.photoAssetID, in: original)
            XCTAssertEqual(slot.caption, expected)
        }
    }

    func test_captionsSurviveManyReorders_unchanged() {
        var current = slots((0..<28).map { "caption \($0)" })
        let before = Dictionary(uniqueKeysWithValues: current.map { ($0.photoAssetID, $0.caption) })

        for index in 0..<50 {
            current = RoomPhotoOrder.swapping(current, from: index % 28, to: (index * 11 + 5) % 28)
        }

        XCTAssertEqual(current.count, 28)
        for slot in current {
            XCTAssertEqual(slot.caption, before[slot.photoAssetID], "\(slot.photoAssetID) kept its own caption")
        }
    }

    func test_settingAfterAReorder_landsOnThePhotographNotTheSlot() {
        let original = slots(["zero", "one", "two"])
        let reordered = RoomPhotoOrder.swapping(original, from: 0, to: 2)
        let updated = RoomPhotoCaptions.setting("new", forAssetID: "a0", in: reordered)

        XCTAssertEqual(RoomPhotoCaptions.caption(forAssetID: "a0", in: updated), "new")
        XCTAssertEqual(RoomPhotoCaptions.caption(forAssetID: "a2", in: updated), "two", "the photograph now in slot 0 is untouched")
    }
}
