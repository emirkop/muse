package config

import "testing"

func TestLoad_DefaultsToDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "")

	cfg := Load()

	if cfg.Environment != Development {
		t.Fatalf("expected default environment %q, got %q", Development, cfg.Environment)
	}
}

func TestLoad_ReadsRecognizedEnvironments(t *testing.T) {
	cases := []Environment{Development, Staging, Production}

	for _, want := range cases {
		t.Setenv("APP_ENV", string(want))

		got := Load()

		if got.Environment != want {
			t.Fatalf("APP_ENV=%q: expected %q, got %q", want, want, got.Environment)
		}
	}
}

func TestLoad_UnrecognizedValueFallsBackToDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "not-a-real-environment")

	cfg := Load()

	if cfg.Environment != Development {
		t.Fatalf("expected fallback environment %q, got %q", Development, cfg.Environment)
	}
}

func TestLoad_DefaultsPortWhenUnset(t *testing.T) {
	t.Setenv("APP_PORT", "")

	cfg := Load()

	if cfg.Port != defaultPort {
		t.Fatalf("expected default port %q, got %q", defaultPort, cfg.Port)
	}
}

func TestLoad_ReadsConfiguredPort(t *testing.T) {
	t.Setenv("APP_PORT", "9090")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Fatalf("expected port %q, got %q", "9090", cfg.Port)
	}
}

func TestLoad_DatabaseURLDefaultsToEmpty(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	cfg := Load()

	if cfg.DatabaseURL != "" {
		t.Fatalf("expected empty DatabaseURL by default, got %q", cfg.DatabaseURL)
	}
}

func TestLoad_ReadsConfiguredDatabaseURL(t *testing.T) {
	const want = "postgres://user:pass@localhost:5432/muse_dev"
	t.Setenv("DATABASE_URL", want)

	cfg := Load()

	if cfg.DatabaseURL != want {
		t.Fatalf("expected DatabaseURL %q, got %q", want, cfg.DatabaseURL)
	}
}

func TestLoad_IdentityConfigDefaultsToEmpty(t *testing.T) {
	t.Setenv("APPLE_BUNDLE_ID", "")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("SESSION_SIGNING_KEY", "")

	cfg := Load()

	if cfg.AppleBundleID != "" || cfg.GoogleClientID != "" || cfg.SessionSigningKey != "" {
		t.Fatalf("expected empty identity config by default, got %+v", cfg)
	}
}

func TestLoad_ReadsConfiguredIdentityConfig(t *testing.T) {
	t.Setenv("APPLE_BUNDLE_ID", "com.muse.app")
	t.Setenv("GOOGLE_CLIENT_ID", "example.apps.googleusercontent.com")
	t.Setenv("SESSION_SIGNING_KEY", "test-signing-key")

	cfg := Load()

	if cfg.AppleBundleID != "com.muse.app" {
		t.Fatalf("expected AppleBundleID %q, got %q", "com.muse.app", cfg.AppleBundleID)
	}
	if cfg.GoogleClientID != "example.apps.googleusercontent.com" {
		t.Fatalf("expected GoogleClientID %q, got %q", "example.apps.googleusercontent.com", cfg.GoogleClientID)
	}
	if cfg.SessionSigningKey != "test-signing-key" {
		t.Fatalf("expected SessionSigningKey %q, got %q", "test-signing-key", cfg.SessionSigningKey)
	}
}
