module muse-backend

go 1.26

// (Security Hardening): pinned to the newest 1.26 patch release.
// `govulncheck ./...` against go1.26.0 reported 24 reachable standard-library
// vulnerabilities (crypto/x509 name-constraint auth bypass GO-2026-4866 on
// the App Store JWS chain verifier, html/template escaper bypasses on the
// share landing pages, crypto/tls and net/http hardening) — every one fixed
// between go1.26.1 and go1.26.6. With GOTOOLCHAIN=auto (the default) the go
// command downloads this toolchain when the installed one is older, so the
// pin is enforced by the build, not by the developer remembering.
toolchain go1.26.6

require (
	github.com/aws/aws-sdk-go-v2 v1.43.7
	github.com/aws/aws-sdk-go-v2/credentials v1.19.37
	github.com/aws/aws-sdk-go-v2/service/s3 v1.107.3
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/jackc/pgx/v5 v5.10.0
	golang.org/x/crypto v0.55.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.38 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.38 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.39 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.31 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.38 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.39 // indirect
	github.com/aws/smithy-go v1.27.8 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
