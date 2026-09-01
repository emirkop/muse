package domain

type Provider string

const (
	ProviderApple  Provider = "apple"
	ProviderGoogle Provider = "google"
)

type AccountID string

type ExternalIdentity struct {
	Provider      Provider
	Subject       string
	Email         string
	EmailVerified bool
}
