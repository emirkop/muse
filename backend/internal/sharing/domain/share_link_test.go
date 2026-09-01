package domain

import (
	"strings"
	"testing"
	"time"
)

const realShapedCode = "aB3-_xYz01234567890abc"

func TestIsPlausibleCode_AcceptsExactlyTheShapeTheGeneratorProduces(t *testing.T) {
	if len(realShapedCode) != 22 {
		t.Fatalf("the fixture is %d characters; the generator produces 22", len(realShapedCode))
	}
	if !IsPlausibleCode(realShapedCode) {
		t.Fatal("a real-shaped code was refused")
	}
	for _, code := range []string{
		strings.Repeat("a", 22),
		strings.Repeat("Z", 22),
		strings.Repeat("9", 22),
		strings.Repeat("-", 22),
		strings.Repeat("_", 22),
	} {
		if !IsPlausibleCode(code) {
			t.Errorf("%q is within the alphabet and must be accepted", code)
		}
	}
}

func TestIsPlausibleCode_LengthIsExact(t *testing.T) {
	for _, length := range []int{0, 1, 21, 23, 36, 100} {
		code := strings.Repeat("a", length)
		if IsPlausibleCode(code) {
			t.Errorf("a %d-character code was accepted; the shape is exactly 22", length)
		}
	}
}

func TestIsPlausibleCode_RefusesEveryOtherShape(t *testing.T) {
	cases := map[string]string{
		"a UUID":                "11111111-2222-4333-8444-555555555555",
		"base64 padding":        "aB3-_xYz01234567890a=",
		"standard base64 plus":  "aB3+_xYz01234567890abc",
		"standard base64 slash": "aB3/_xYz01234567890abc",
		"a path traversal":      "../../../etc/passwd0000",
		"a SQL fragment":        "' OR 1=1 --0000000000",
		"whitespace":            "aB3-_xYz0123456789 abc",
		"a null byte":           "aB3-_xYz01234567890a\x00",
		"a newline":             "aB3-_xYz01234567890a\n",
		"non-ASCII":             "aB3-_xYz01234567890é",
		"an empty string":       "",
	}
	for name, code := range cases {
		if IsPlausibleCode(code) {
			t.Errorf("%s (%q) was accepted", name, code)
		}
	}
}

func TestShareLink_IsActiveReadsStatusNotTheTimestamp(t *testing.T) {
	revokedAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		link   ShareLink
		active bool
	}{
		{"active, no timestamp", ShareLink{Status: StatusActive}, true},
		{"revoked, with timestamp", ShareLink{Status: StatusRevoked, RevokedAt: &revokedAt}, false},
		{"revoked but no timestamp", ShareLink{Status: StatusRevoked}, false},
		{"active but with a timestamp", ShareLink{Status: StatusActive, RevokedAt: &revokedAt}, true},
		{"zero value is not active", ShareLink{}, false},
		{"an unrecognised status is not active", ShareLink{Status: Status("something_else")}, false},
	}
	for _, testCase := range cases {
		if got := testCase.link.IsActive(); got != testCase.active {
			t.Errorf("%s: IsActive() = %v, want %v", testCase.name, got, testCase.active)
		}
	}
}

func TestCollectionShareLink_IsActiveMatchesTheMuseumLinkExactly(t *testing.T) {
	revokedAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	statuses := []Status{StatusActive, StatusRevoked, Status(""), Status("other")}

	for _, status := range statuses {
		for _, timestamp := range []*time.Time{nil, &revokedAt} {
			museum := ShareLink{Status: status, RevokedAt: timestamp}
			collection := CollectionShareLink{Status: status, RevokedAt: timestamp}
			if museum.IsActive() != collection.IsActive() {
				t.Errorf("status %q (revokedAt set: %v): museum says %v, collection says %v — the two link kinds must agree",
					status, timestamp != nil, museum.IsActive(), collection.IsActive())
			}
		}
	}
}
