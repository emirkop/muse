import Foundation

public protocol SessionStoring: Sendable {
    func save(_ session: AuthSession) throws

    func loadRefreshToken() throws -> String?

    func clear() throws
}
