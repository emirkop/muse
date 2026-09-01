package observability

const (
	EventLoginRefused          = "authn.login_refused"
	EventLoginFailed           = "authn.login_failed"
	EventRefreshReuseDetected  = "authn.refresh_reuse_detected"
	EventAuthenticationRefused = "authn.request_refused"
	EventThrottled             = "authn.throttled"
	EventThrottleWriteFailed   = "authn.throttle_write_failed"

	EventAuthorizationRefused = "authz.refused"

	EventEntitlementVerificationFailed      = "entitlement.verification_failed"
	EventEntitlementVerificationUnavailable = "entitlement.verification_unavailable"
	EventEntitlementNotApplicable           = "entitlement.not_applicable"
	EventEntitlementBoundElsewhere          = "entitlement.bound_to_another_account"
	EventEntitlementCapacityReached         = "entitlement.capacity_reached"

	EventShareLinkUnavailable = "sharing.link_unavailable"
	EventShareLinkFailed      = "sharing.link_failed"

	EventAssetBundleNotPublished   = "asset_delivery.bundle_not_published"
	EventAssetDeliveryUnconfigured = "asset_delivery.unconfigured"
	EventAssetPublishFailed        = "asset_delivery.publish_failed"

	EventMediaTicketRejected = "media.ticket_rejected"
	EventMediaUploadRefused  = "media.upload_refused"
	EventMediaStorageFailed  = "media.storage_failed"
	EventMediaReclaimFailed  = "media.reclaim_failed"

	EventPersistenceFailed     = "persistence.failed"
	EventDependencyUnavailable = "config.dependency_unavailable"
	EventEmailDeliveryFailed   = "email.delivery_failed"
)

const (
	ReasonNoMuseumForCaller  = "no_museum_for_caller"
	ReasonNotOwner           = "not_owner"
	ReasonNotVisible         = "not_visible"
	ReasonMalformedID        = "malformed_id"
	ReasonNotFound           = "not_found"
	ReasonNoActiveLink       = "no_active_link"
	ReasonNoCredential       = "no_credential"
	ReasonCredentialRejected = "credential_rejected"
	ReasonNotConfigured      = "not_configured"
)
