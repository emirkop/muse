import XCTest
@testable import MuseApp

@MainActor
final class SignInWithAppleUseCaseTests: XCTestCase {
    func test_execute_success_savesSessionAndReturnsIt() async throws {
        let expectedResult = LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: true
        )
        let identityProvider = FakeAppleIdentityProvider()
        let authService = FakeAuthenticationService()
        authService.loginResult = .success(expectedResult)
        let sessionStore = FakeSessionStore()
        let useCase = SignInWithAppleUseCase(identityProvider: identityProvider, authService: authService, sessionStore: sessionStore)

        let result = try await useCase.execute()

        XCTAssertEqual(result, expectedResult)
        XCTAssertEqual(sessionStore.savedSession, expectedResult.session)
        XCTAssertEqual(authService.receivedIdentityToken, "apple-token")
        XCTAssertEqual(authService.receivedNonce, "nonce")
    }

    func test_execute_existingIdentity_reportsIsNewAccountFalse() async throws {
        let expectedResult = LoginResult(
            session: AuthSession(accessToken: "access", accessTokenExpiresAt: Date(), refreshToken: "refresh", refreshTokenExpiresAt: Date()),
            isNewAccount: false
        )
        let authService = FakeAuthenticationService()
        authService.loginResult = .success(expectedResult)
        let useCase = SignInWithAppleUseCase(identityProvider: FakeAppleIdentityProvider(), authService: authService, sessionStore: FakeSessionStore())

        let result = try await useCase.execute()

        XCTAssertFalse(result.isNewAccount)
    }

    func test_execute_appleCancellation_propagatesWithoutCallingBackend() async {
        let identityProvider = FakeAppleIdentityProvider()
        identityProvider.result = .failure(AppleSignInError.cancelled)
        let authService = FakeAuthenticationService()
        let sessionStore = FakeSessionStore()
        let useCase = SignInWithAppleUseCase(identityProvider: identityProvider, authService: authService, sessionStore: sessionStore)

        do {
            _ = try await useCase.execute()
            XCTFail("expected cancellation to propagate")
        } catch AppleSignInError.cancelled {
        } catch {
            XCTFail("expected AppleSignInError.cancelled, got \(error)")
        }

        XCTAssertNil(authService.receivedIdentityToken, "backend must not be called when Apple sign-in is cancelled")
        XCTAssertNil(sessionStore.savedSession)
    }

    func test_execute_backendFailure_doesNotPersistSession() async {
        let identityProvider = FakeAppleIdentityProvider()
        let authService = FakeAuthenticationService()
        authService.loginResult = .failure(IdentityAPIClientError.server(statusCode: 401, message: "authentication failed"))
        let sessionStore = FakeSessionStore()
        let useCase = SignInWithAppleUseCase(identityProvider: identityProvider, authService: authService, sessionStore: sessionStore)

        do {
            _ = try await useCase.execute()
            XCTFail("expected backend failure to propagate")
        } catch {
        }

        XCTAssertNil(sessionStore.savedSession)
    }
}
