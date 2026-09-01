import Foundation

@MainActor
public final class AuthenticationViewModel {
    public enum State: Equatable {
        case idle
        case loading
        case failed(message: String)
        case succeeded(LoginResult)
    }

    public private(set) var state: State = .idle {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let appleSignInUseCase: SignInWithAppleUseCase
    private let googleSignInUseCase: SignInWithGoogleUseCase

    public init(appleSignInUseCase: SignInWithAppleUseCase, googleSignInUseCase: SignInWithGoogleUseCase) {
        self.appleSignInUseCase = appleSignInUseCase
        self.googleSignInUseCase = googleSignInUseCase
    }

    public func signInWithApple() async {
        state = .loading
        do {
            let result = try await appleSignInUseCase.execute()
            state = .succeeded(result)
        } catch AppleSignInError.cancelled {
            state = .idle
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: genericFailureMessage))
        }
    }

    public func signInWithGoogle() async {
        state = .loading
        do {
            let result = try await googleSignInUseCase.execute()
            state = .succeeded(result)
        } catch GoogleSignInProviderError.cancelled {
            state = .idle
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: genericFailureMessage))
        }
    }

    private var genericFailureMessage: String { "Something went wrong signing in." }
}
