import XCTest
@testable import MuseApp

@MainActor
final class SignInWithGoogleUseCaseTests: XCTestCase {
    func test_execute_success_savesSessionAndReturnsIt() async throws {
        let expectedResult = LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: true
        )
        let identityProvider = FakeGoogleIdentityProvider()
        let authService = FakeAuthenticationService()
        authService.loginResult = .success(expectedResult)
        let sessionStore = FakeSessionStore()
        let useCase = SignInWithGoogleUseCase(identityProvider: identityProvider, authService: authService, sessionStore: sessionStore)

        let result = try await useCase.execute()

        XCTAssertEqual(result, expectedResult)
        XCTAssertEqual(sessionStore.savedSession, expectedResult.session)
        XCTAssertEqual(authService.receivedIdentityToken, "google-token")
        XCTAssertEqual(authService.googleCallCount, 1)
    }

    func test_execute_existingIdentity_reportsIsNewAccountFalse() async throws {
        let expectedResult = LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: false
        )
        let authService = FakeAuthenticationService()
        authService.loginResult = .success(expectedResult)
        let useCase = SignInWithGoogleUseCase(identityProvider: FakeGoogleIdentityProvider(), authService: authService, sessionStore: FakeSessionStore())

        let result = try await useCase.execute()

        XCTAssertFalse(result.isNewAccount)
    }

    func test_execute_googleCancellation_propagatesWithoutCallingBackend() async {
        let identityProvider = FakeGoogleIdentityProvider()
        identityProvider.result = .failure(GoogleSignInProviderError.cancelled)
        let authService = FakeAuthenticationService()
        let sessionStore = FakeSessionStore()
        let useCase = SignInWithGoogleUseCase(identityProvider: identityProvider, authService: authService, sessionStore: sessionStore)

        do {
            _ = try await useCase.execute()
            XCTFail("expected cancellation to propagate")
        } catch GoogleSignInProviderError.cancelled {
        } catch {
            XCTFail("expected GoogleSignInProviderError.cancelled, got \(error)")
        }

        XCTAssertEqual(authService.googleCallCount, 0, "backend must not be called when Google sign-in is cancelled")
        XCTAssertNil(sessionStore.savedSession)
    }

    func test_execute_backendFailure_doesNotPersistSession() async {
        let identityProvider = FakeGoogleIdentityProvider()
        let authService = FakeAuthenticationService()
        authService.loginResult = .failure(IdentityAPIClientError.server(statusCode: 401, message: "authentication failed"))
        let sessionStore = FakeSessionStore()
        let useCase = SignInWithGoogleUseCase(identityProvider: identityProvider, authService: authService, sessionStore: sessionStore)

        do {
            _ = try await useCase.execute()
            XCTFail("expected backend failure to propagate")
        } catch {
        }

        XCTAssertNil(sessionStore.savedSession)
    }
}
