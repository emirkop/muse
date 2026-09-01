import XCTest
@testable import MuseApp

final class EmailCredentialRulesTests: XCTestCase {

    // MARK: - Bounds mirror the server

    func test_passwordBoundsMirrorTheServer() {
        XCTAssertEqual(EmailCredentialRules.passwordMinimumLength, 10)
        XCTAssertEqual(EmailCredentialRules.passwordMaximumLength, 128)
    }

    // MARK: - Email shape

    func test_acceptsOrdinaryAddresses() {
        for address in [
            "someone@example.com",
            "a.b+muse@example.co.uk",
            "  Mixed.Case@Example.COM  ",
            "üser@example.com"
        ] {
            XCTAssertTrue(EmailCredentialRules.isPlausibleEmail(address), "should accept \(address)")
        }
    }

    func test_rejectsMalformedAddresses() {
        for address in [
            "", "   ", "nobody.example.com", "a@b@example.com", "@example.com",
            "someone@", "someone@localhost", "someone@.example.com",
            "someone@example.com.", "some one@example.com", "a,b@example.com"
        ] {
            XCTAssertFalse(EmailCredentialRules.isPlausibleEmail(address), "should reject \(address)")
        }
    }

    func test_normalisationMatchesTheServers() {
        XCTAssertEqual(EmailCredentialRules.normalised("  Emir.Test@Example.COM \n"), "emir.test@example.com")
        XCTAssertEqual(
            EmailCredentialRules.normalised("a.b+muse@example.com"), "a.b+muse@example.com",
            "dots and +tags are preserved, exactly as the server preserves them"
        )
    }

    // MARK: - Sign up

    func test_signUpAcceptsAValidForm() {
        XCTAssertNil(EmailCredentialRules.signUpProblem(
            email: "someone@example.com", password: "a-good-passphrase", confirmation: "a-good-passphrase"
        ))
        XCTAssertTrue(EmailCredentialRules.canAttemptSignUp(
            email: "someone@example.com", password: "a-good-passphrase", confirmation: "a-good-passphrase"
        ))
    }

    func test_signUpReportsProblemsInFieldOrder() {
        XCTAssertEqual(
            EmailCredentialRules.signUpProblem(email: "bad", password: "short", confirmation: "mismatch"),
            .emailMalformed
        )
        XCTAssertEqual(
            EmailCredentialRules.signUpProblem(email: "ok@example.com", password: "short", confirmation: "mismatch"),
            .passwordTooShort
        )
        XCTAssertEqual(
            EmailCredentialRules.signUpProblem(
                email: "ok@example.com", password: "a-good-passphrase", confirmation: "different"
            ),
            .passwordsDoNotMatch
        )
    }

    func test_signUpEnforcesTheSameMinimumAsTheServer() {
        let oneShort = String(repeating: "a", count: EmailCredentialRules.passwordMinimumLength - 1)
        let exactly = String(repeating: "a", count: EmailCredentialRules.passwordMinimumLength)

        XCTAssertEqual(
            EmailCredentialRules.signUpProblem(email: "a@b.com", password: oneShort, confirmation: oneShort),
            .passwordTooShort
        )
        XCTAssertNil(EmailCredentialRules.signUpProblem(email: "a@b.com", password: exactly, confirmation: exactly))
    }

    func test_signUpRejectsAnOverLongPassword() {
        let tooLong = String(repeating: "a", count: EmailCredentialRules.passwordMaximumLength + 1)

        XCTAssertEqual(
            EmailCredentialRules.signUpProblem(email: "a@b.com", password: tooLong, confirmation: tooLong),
            .passwordTooLong
        )
    }

    func test_signUpImposesNoCompositionRules() {
        for password in ["aaaaaaaaaa", "correct horse battery staple", "0123456789"] {
            XCTAssertNil(
                EmailCredentialRules.signUpProblem(email: "a@b.com", password: password, confirmation: password),
                "\(password) must be accepted"
            )
        }
    }

    // MARK: - Log in

    func test_logInDoesNotApplyThePasswordPolicy() {
        XCTAssertNil(
            EmailCredentialRules.logInProblem(email: "someone@example.com", password: "old"),
            "an old, short password must still be submittable — the server decides"
        )
        XCTAssertTrue(EmailCredentialRules.canAttemptLogIn(email: "someone@example.com", password: "old"))
    }

    func test_logInRequiresANonEmptyPassword() {
        XCTAssertFalse(EmailCredentialRules.canAttemptLogIn(email: "someone@example.com", password: ""))
    }

    func test_logInRequiresAPlausibleAddress() {
        XCTAssertEqual(EmailCredentialRules.logInProblem(email: "nope", password: "anything"), .emailMalformed)
        XCTAssertFalse(EmailCredentialRules.canAttemptLogIn(email: "nope", password: "anything"))
    }

    // MARK: - Reset

    func test_resetAppliesThePasswordPolicyAndTheMatchCheck() {
        XCTAssertEqual(EmailCredentialRules.resetProblem(password: "short", confirmation: "short"), .passwordTooShort)
        XCTAssertEqual(
            EmailCredentialRules.resetProblem(password: "a-good-passphrase", confirmation: "other"),
            .passwordsDoNotMatch
        )
        XCTAssertNil(EmailCredentialRules.resetProblem(password: "a-good-passphrase", confirmation: "a-good-passphrase"))
    }

    // MARK: - Copy

    func test_problemMessagesNameTheActualBound() {
        XCTAssertTrue(
            EmailCredentialRules.Problem.passwordTooShort.message
                .contains("\(EmailCredentialRules.passwordMinimumLength)")
        )
    }
}
