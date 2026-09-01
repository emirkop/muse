import Foundation

public protocol EntitlementServicing: Sendable {
    func fetchEntitlement(accessToken: String) async throws -> AccountEntitlement

    func appAccountToken(accessToken: String) async throws -> UUID

    func redeem(accessToken: String, signedTransaction: String) async throws -> AccountEntitlement
}
