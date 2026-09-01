import GoogleSignIn
import UIKit

@MainActor
public final class GoogleSignInProvider: GoogleIdentityProviding {
    public init() {}

    public func requestIdentity() async throws -> GoogleIdentity {
        guard let presentingViewController = Self.topViewController() else {
            throw GoogleSignInProviderError.failed
        }

        let result: GIDSignInResult
        do {
            result = try await GIDSignIn.sharedInstance.signIn(withPresenting: presentingViewController)
        } catch let error as GIDSignInError where error.code == .canceled {
            throw GoogleSignInProviderError.cancelled
        } catch {
            throw GoogleSignInProviderError.failed
        }

        guard let identityToken = result.user.idToken?.tokenString else {
            throw GoogleSignInProviderError.failed
        }
        return GoogleIdentity(identityToken: identityToken)
    }

    private static func topViewController() -> UIViewController? {
        let keyWindow = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap { $0.windows }
            .first { $0.isKeyWindow }

        var top = keyWindow?.rootViewController
        while let presented = top?.presentedViewController {
            top = presented
        }
        return top
    }
}

extension GoogleSignInProvider {
    public static func handleRedirectURL(_ url: URL) -> Bool {
        GIDSignIn.sharedInstance.handle(url)
    }
}
