import Foundation

@MainActor
public final class EmailAuthViewModel {
    public enum State: Equatable {
        case idle
        case working
        case failed(message: String)
        case acknowledged(message: String)
        case authenticated(LoginResult)
    }

    public private(set) var state: State = .idle {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let authService: any AuthenticationServicing
    private let sessionStore: any SessionStoring

    public init(authService: any AuthenticationServicing, sessionStore: any SessionStoring) {
        self.authService = authService
        self.sessionStore = sessionStore
    }

    // MARK: - Sign up

    public func signUp(email: String, password: String, confirmation: String) async {
        if let problem = EmailCredentialRules.signUpProblem(email: email, password: password, confirmation: confirmation) {
            state = .failed(message: problem.message)
            return
        }
        state = .working
        do {
            try await authService.signUpWithEmail(email: EmailCredentialRules.normalised(email), password: password)
            state = .acknowledged(message: "Check your email for a link to finish creating your account.")
        } catch {
            state = .failed(message: Self.message(for: error))
        }
    }

    // MARK: - Verification

    public func verify(token: String) async {
        let trimmed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            state = .failed(message: "Paste the code from your email.")
            return
        }
        state = .working
        do {
            let result = try await authService.verifyEmail(token: trimmed)
            try? sessionStore.save(result.session)
            state = .authenticated(result)
        } catch {
            state = .failed(message: Self.message(for: error))
        }
    }

    public func resendVerification(email: String) async {
        guard EmailCredentialRules.isPlausibleEmail(email) else {
            state = .failed(message: EmailCredentialRules.Problem.emailMalformed.message)
            return
        }
        state = .working
        do {
            try await authService.resendVerification(email: EmailCredentialRules.normalised(email))
            state = .acknowledged(message: "We've sent another link. It replaces the previous one.")
        } catch {
            state = .failed(message: Self.message(for: error))
        }
    }

    // MARK: - Log in

    public func logIn(email: String, password: String) async {
        if let problem = EmailCredentialRules.logInProblem(email: email, password: password) {
            state = .failed(message: problem.message)
            return
        }
        state = .working
        do {
            let result = try await authService.logInWithEmail(
                email: EmailCredentialRules.normalised(email),
                password: password
            )
            try? sessionStore.save(result.session)
            state = .authenticated(result)
        } catch {
            state = .failed(message: Self.message(for: error))
        }
    }

    // MARK: - Password reset

    public func requestPasswordReset(email: String) async {
        guard EmailCredentialRules.isPlausibleEmail(email) else {
            state = .failed(message: EmailCredentialRules.Problem.emailMalformed.message)
            return
        }
        state = .working
        do {
            try await authService.requestPasswordReset(email: EmailCredentialRules.normalised(email))
            state = .acknowledged(message: "If an account exists for that address, we've sent a reset link.")
        } catch {
            state = .failed(message: Self.message(for: error))
        }
    }

    public func confirmPasswordReset(token: String, password: String, confirmation: String) async {
        let trimmed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            state = .failed(message: "Paste the code from your email.")
            return
        }
        if let problem = EmailCredentialRules.resetProblem(password: password, confirmation: confirmation) {
            state = .failed(message: problem.message)
            return
        }
        state = .working
        do {
            try await authService.confirmPasswordReset(token: trimmed, password: password)
            state = .acknowledged(message: "Your password is changed. You've been signed out everywhere — log in with your new password.")
        } catch {
            state = .failed(message: Self.message(for: error))
        }
    }

    // MARK: - Error copy

    static func message(for error: Error) -> String {
        guard let apiError = error as? IdentityAPIClientError else {
            return "Something went wrong. Please try again."
        }
        switch apiError {
        case .offline:
            return "You're offline. Signing in needs a connection."
        case .cancelled:
            return ""
        case .transport:
            return "Couldn't reach Muse. Check your connection and try again."
        case .invalidResponse:
            return "Something went wrong. Please try again."
        case .server(let statusCode, let message):
            switch statusCode {
            case 401:
                return "Email or password is incorrect."
            case 429:
                return message ?? "Too many attempts. Please try again later."
            case 400:
                return message ?? "Please check what you entered and try again."
            case 503:
                return "Email sign-in isn't available right now. You can continue with Apple or Google."
            default:
                return "Something went wrong. Please try again."
            }
        }
    }
}
