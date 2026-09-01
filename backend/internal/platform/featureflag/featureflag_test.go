package featureflag_test

import (
	"strings"
	"testing"

	"muse-backend/internal/platform/featureflag"
)

func TestDefaults_AreTheDeclaredSafeValue(t *testing.T) {
	for _, environment := range []string{"development", "staging", "production", ""} {
		provider, err := featureflag.NewProvider(environment, nil)
		if err != nil {
			t.Fatalf("%s: %v", environment, err)
		}
		if provider.IsEnabled(featureflag.VisitorAudibleRoomMusic) {
			t.Errorf("%s: visitor-audible Room music must default OFF — 's clearance is unresolved",
				environment)
		}
		for _, status := range provider.Snapshot() {
			if status.Enabled != status.Default {
				t.Errorf("%s: %s reported enabled=%v with nothing set, want its default %v",
					environment, status.Name, status.Enabled, status.Default)
			}
			if status.Overridden {
				t.Errorf("%s: %s reported as overridden with an empty environment", environment, status.Name)
			}
		}
	}
}

func TestOverride_TakesEffectInNonProduction(t *testing.T) {
	provider, err := featureflag.NewProvider("development",
		[]string{"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !provider.IsEnabled(featureflag.VisitorAudibleRoomMusic) {
		t.Fatal("an explicit enabled override was not honoured")
	}

	provider, err = featureflag.NewProvider("development",
		[]string{"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.IsEnabled(featureflag.VisitorAudibleRoomMusic) {
		t.Fatal("an explicit disabled override was not honoured")
	}
}

func TestProduction_DefaultsSafeAndReportsClearance(t *testing.T) {
	provider, err := featureflag.NewProvider("production", []string{
		"PATH=/usr/bin", "DATABASE_URL=postgres://example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.IsEnabled(featureflag.VisitorAudibleRoomMusic) {
		t.Fatal("production must default to visitor-audible music OFF")
	}

	enabled, err := featureflag.NewProvider("production",
		[]string{"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.IsEnabled(featureflag.VisitorAudibleRoomMusic) {
		t.Fatal("production must be able to enable the flag once clearance is confirmed — " +
			"a gate that cannot be opened is not a flag")
	}
	for _, status := range enabled.Snapshot() {
		if status.Name != "visitor_audible_room_music" {
			continue
		}
		if !status.RequiresExternalClearance || status.ClearanceRequired == "" {
			t.Error("enabling this flag in production must be reported as requiring external clearance, " +
				"naming what has to be true first")
		}
	}
}

func TestUnknownFlag_IsAlwaysDisabled(t *testing.T) {
	provider, err := featureflag.NewProvider("development",
		[]string{"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=enabled"})
	if err != nil {
		t.Fatal(err)
	}
	var unknown featureflag.Flag
	if provider.IsEnabled(unknown) {
		t.Fatal("the zero Flag must be disabled — an unknown flag can never enable behaviour")
	}
	if unknown.Name() != "" || unknown.EnvironmentVariable() != "" {
		t.Errorf("the zero Flag should name nothing, got %q / %q", unknown.Name(), unknown.EnvironmentVariable())
	}
}

func TestUnknownVariable_RefusesToBuildAProvider(t *testing.T) {
	cases := map[string][]string{
		"misspelled flag name": {"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIK=enabled"},
		"invented flag":        {"MUSE_FLAG_SKIP_AUTHENTICATION=enabled"},
		"prefix only":          {"MUSE_FLAG_=enabled"},
	}
	for name, environ := range cases {
		provider, err := featureflag.NewProvider("production", environ)
		if err == nil {
			t.Errorf("%s: expected a configuration error, got a working provider", name)
			continue
		}
		if provider != nil {
			t.Errorf("%s: a rejected configuration must not yield a provider", name)
		}
		if !strings.Contains(err.Error(), "names no known flag") {
			t.Errorf("%s: error should say the variable names no known flag, got %q", name, err)
		}
	}
}

func TestUnparseableValue_RefusesToBuildAProvider(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "on", "ture", "", "ENABLED?"} {
		_, err := featureflag.NewProvider("production",
			[]string{"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=" + value})
		if err == nil {
			t.Errorf("value %q was accepted; only %q and %q may parse", value, "enabled", "disabled")
		}
	}
	for _, value := range []string{"Enabled", "ENABLED", " enabled "} {
		provider, err := featureflag.NewProvider("production",
			[]string{"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=" + value})
		if err != nil {
			t.Errorf("value %q should be accepted: %v", value, err)
			continue
		}
		if !provider.IsEnabled(featureflag.VisitorAudibleRoomMusic) {
			t.Errorf("value %q parsed but did not enable the flag", value)
		}
	}
}

func TestRegistry_IsCompleteAndSelfDescribing(t *testing.T) {
	flags := featureflag.AllFlags()
	if len(flags) == 0 {
		t.Fatal("no flags declared — this test would pass vacuously")
	}
	provider, err := featureflag.NewProvider("development", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := provider.Snapshot()
	if len(snapshot) != len(flags) {
		t.Fatalf("%d flags declared but %d in the snapshot — a flag has no definition",
			len(flags), len(snapshot))
	}
	for _, status := range snapshot {
		if status.Name == "" || status.Summary == "" {
			t.Errorf("flag %q must carry a name and a one-line summary", status.Name)
		}
		if !strings.HasPrefix(status.EnvironmentVariable, "MUSE_FLAG_") {
			t.Errorf("flag %q: variable %q should carry the MUSE_FLAG_ prefix",
				status.Name, status.EnvironmentVariable)
		}
		if status.RequiresExternalClearance && status.ClearanceRequired == "" {
			t.Errorf("flag %q claims to require external clearance but does not say what", status.Name)
		}
		if status.Default {
			t.Errorf("flag %q defaults ON — a provisional capability must default to its safe value; "+
				"if the behaviour is stable it should not be a flag at all (instruction)",
				status.Name)
		}
	}
}
