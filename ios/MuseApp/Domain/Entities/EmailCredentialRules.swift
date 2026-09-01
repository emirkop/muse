import Foundation

public enum EmailCredentialRules {
    public static let passwordMinimumLength = 10
    public static let passwordMaximumLength = 128

    public enum Problem: Equatable, Sendable {
        case emailMalformed
        case passwordTooShort
        case passwordTooLong
        case passwordsDoNotMatch

        public var message: String {
            switch self {
            case .emailMalformed:
                return "Enter a valid email address."
            case .passwordTooShort:
                return "Use at least \(EmailCredentialRules.passwordMinimumLength) characters."
            case .passwordTooLong:
                return "Use at most \(EmailCredentialRules.passwordMaximumLength) characters."
            case .passwordsDoNotMatch:
                return "These passwords don't match."
            }
        }
    }

    public static func isPlausibleEmail(_ raw: String) -> Bool {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed.count <= 254 else { return false }
        guard !trimmed.contains(where: { $0.isWhitespace || $0 == "," || $0 == ";" }) else { return false }

        let parts = trimmed.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2 else { return false }
        let local = parts[0]
        let domain = parts[1]
        guard !local.isEmpty, !domain.isEmpty else { return false }
        guard domain.contains("."), !domain.hasPrefix("."), !domain.hasSuffix(".") else { return false }
        return true
    }

    public static func normalised(_ raw: String) -> String {
        raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }

    public static func logInProblem(email: String, password: String) -> Problem? {
        guard isPlausibleEmail(email) else { return .emailMalformed }
        return nil
    }

    public static func canAttemptLogIn(email: String, password: String) -> Bool {
        isPlausibleEmail(email) && !password.isEmpty
    }

    public static func signUpProblem(email: String, password: String, confirmation: String) -> Problem? {
        guard isPlausibleEmail(email) else { return .emailMalformed }
        if password.count < passwordMinimumLength { return .passwordTooShort }
        if password.count > passwordMaximumLength { return .passwordTooLong }
        if password != confirmation { return .passwordsDoNotMatch }
        return nil
    }

    public static func canAttemptSignUp(email: String, password: String, confirmation: String) -> Bool {
        signUpProblem(email: email, password: password, confirmation: confirmation) == nil
    }

    public static func resetProblem(password: String, confirmation: String) -> Problem? {
        if password.count < passwordMinimumLength { return .passwordTooShort }
        if password.count > passwordMaximumLength { return .passwordTooLong }
        if password != confirmation { return .passwordsDoNotMatch }
        return nil
    }
}
