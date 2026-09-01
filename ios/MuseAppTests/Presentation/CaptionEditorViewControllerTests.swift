import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class CaptionEditorViewControllerTests: XCTestCase {

    private func makeEditor(
        caption: String = "",
        outcome: @escaping (String) -> CaptionRules.CaptionSaveOutcome = { _ in .saved }
    ) -> (CaptionEditorViewController, () -> [String], () -> Int) {
        var submitted: [String] = []
        var finished = 0
        let editor = CaptionEditorViewController(
            caption: caption,
            save: { text in
                submitted.append(text)
                return outcome(text)
            },
            onFinished: { finished += 1 }
        )
        editor.loadViewIfNeeded()
        return (editor, { submitted }, { finished })
    }

    private func settle() async {
        for _ in 0..<12 { await Task.yield() }
    }

    // MARK: - Entry state

    func test_opensWithThePhotographsExistingCaption() {
        let (editor, _, _) = makeEditor(caption: "Trabzon, 1998")
        XCTAssertEqual(editor.testFieldText, "Trabzon, 1998")
        XCTAssertFalse(editor.testSaveEnabled, "nothing to save until something changes")
    }

    func test_anUncaptionedPhotographOpensEmpty() {
        let (editor, _, _) = makeEditor()
        XCTAssertEqual(editor.testFieldText, "")
        XCTAssertFalse(editor.testSaveEnabled)
        XCTAssertNil(editor.testErrorMessage)
    }

    // MARK: - Live feedback

    func test_countIsLiveAndInCharacters() {
        let (editor, _, _) = makeEditor()
        editor.testSetText("café")
        XCTAssertEqual(editor.testCountText, "4 characters", "characters are what the owner is typing")
    }

    func test_overTheInterimBound_savingIsBlockedAndTheReasonIsShown() {
        let (editor, submitted, _) = makeEditor()
        editor.testSetText(String(repeating: "a", count: CaptionRules.interimMaximumBytes + 1))

        XCTAssertFalse(editor.testSaveEnabled)
        XCTAssertNotNil(editor.testErrorMessage)
        editor.testTapSave()
        XCTAssertTrue(submitted().isEmpty, "a blocked save must send nothing")
    }

    func test_atExactlyTheBound_savingIsAllowed() {
        let (editor, _, _) = makeEditor()
        editor.testSetText(String(repeating: "a", count: CaptionRules.interimMaximumBytes))
        XCTAssertTrue(editor.testSaveEnabled)
        XCTAssertNil(editor.testErrorMessage)
    }

    func test_emptyingAnExistingCaption_offersToRemoveIt() {
        let (editor, _, _) = makeEditor(caption: "Was here")
        editor.testSetText("")
        XCTAssertEqual(editor.testSaveTitle, "Remove Caption")
        XCTAssertTrue(editor.testSaveEnabled)
    }

    func test_reTypingTheOriginalCaption_disablesSaveAgain() {
        let (editor, _, _) = makeEditor(caption: "Original")
        editor.testSetText("Changed")
        XCTAssertTrue(editor.testSaveEnabled)
        editor.testSetText("Original")
        XCTAssertFalse(editor.testSaveEnabled, "there is nothing to save")
        editor.testSetText("  Original  ")
        XCTAssertFalse(editor.testSaveEnabled, "trimming makes this the same caption")
    }

    // MARK: - Saving

    func test_savingSubmitsTheTypedText() async {
        let (editor, submitted, finished) = makeEditor()
        editor.testSetText("Trabzon, 1998")
        editor.testTapSave()
        await settle()

        XCTAssertEqual(submitted(), ["Trabzon, 1998"])
        XCTAssertEqual(finished(), 1, "a successful save closes the editor")
    }

    func test_aFailedSave_keepsTheTextAndShowsTheReasonInline() async {
        let (editor, _, finished) = makeEditor(
            caption: "Previous",
            outcome: { _ in .failed(message: "Couldn't save the caption.") }
        )
        editor.testSetText("Typed but not saved")
        editor.testTapSave()
        await settle()

        XCTAssertEqual(editor.testFieldText, "Typed but not saved", "the owner's text is still there")
        XCTAssertEqual(editor.testErrorMessage, "Couldn't save the caption.")
        XCTAssertEqual(finished(), 0, "the editor stays open so the owner can retry")
        XCTAssertTrue(editor.testSaveEnabled, "and retry is possible immediately")
    }

    func test_retryingAfterAFailure_sendsAgain() async {
        var attempt = 0
        let (editor, submitted, finished) = makeEditor(outcome: { _ in
            attempt += 1
            return attempt == 1 ? .failed(message: "Network trouble.") : .saved
        })
        editor.testSetText("Second time lucky")
        editor.testTapSave()
        await settle()
        XCTAssertEqual(editor.testErrorMessage, "Network trouble.")

        editor.testTapSave()
        await settle()

        XCTAssertEqual(submitted(), ["Second time lucky", "Second time lucky"])
        XCTAssertEqual(finished(), 1)
    }

    func test_aRejectionIsShownInlineToo_andKeepsTheText() async {
        let (editor, _, finished) = makeEditor(outcome: { _ in .rejected(message: "That caption is too long.") })
        editor.testSetText("Something")
        editor.testTapSave()
        await settle()

        XCTAssertEqual(editor.testFieldText, "Something")
        XCTAssertEqual(editor.testErrorMessage, "That caption is too long.")
        XCTAssertEqual(finished(), 0)
    }

    func test_editingAfterAFailure_clearsTheError() async {
        let (editor, _, _) = makeEditor(outcome: { _ in .failed(message: "Nope.") })
        editor.testSetText("One")
        editor.testTapSave()
        await settle()
        XCTAssertNotNil(editor.testErrorMessage)

        editor.testSetText("One more")
        XCTAssertNil(editor.testErrorMessage, "typing clears the stale error")
    }

    func test_cancel_sendsNothing() {
        let (editor, submitted, finished) = makeEditor(caption: "Previous")
        editor.testSetText("Abandoned")
        editor.testTapCancel()

        XCTAssertTrue(submitted().isEmpty, "cancel never writes")
        XCTAssertEqual(finished(), 1)
    }

    func test_savingTwiceInARow_sendsOnce() async {
        let (editor, submitted, _) = makeEditor()
        editor.testSetText("Once")
        editor.testTapSave()
        editor.testTapSave()
        await settle()
        XCTAssertEqual(submitted().count, 1, "a double tap must not double-write")
    }
}
