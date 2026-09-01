package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

func testDatabaseURL(t *testing.T, purpose string) string {
	t.Helper()
	connString := os.Getenv("TEST_DATABASE_URL")
	if connString == "" {
		t.Skipf("TEST_DATABASE_URL not set — skipping %s", purpose)
	}
	if err := refuseNonTestDatabase(connString, os.Getenv("DATABASE_URL")); err != nil {
		t.Fatalf("refusing to run %s: %v", purpose, err)
	}
	return connString
}

var protectedDatabaseNames = map[string]bool{
	"muse_dev":        true,
	"muse":            true,
	"muse_production": true,
	"muse_prod":       true,
	"muse_staging":    true,
}

func refuseNonTestDatabase(testURL, developmentURL string) error {
	testName, err := databaseName(testURL)
	if err != nil {
		return fmt.Errorf("TEST_DATABASE_URL is not a usable connection string: %w", err)
	}
	if protectedDatabaseNames[testName] {
		return fmt.Errorf(
			"TEST_DATABASE_URL names %q, which these suites must never touch — they TRUNCATE accounts, "+
				"museums and rooms on every stack, which deletes the DEV account/Museum/Room the design "+
				"phase uses. Point it at a dedicated database (e.g. muse_test): "+
				"createdb muse_test && TEST_DATABASE_URL=postgres://localhost:5432/muse_test?sslmode=disable",
			testName)
	}
	if developmentURL != "" {
		developmentName, err := databaseName(developmentURL)
		if err == nil && developmentName == testName {
			return fmt.Errorf(
				"TEST_DATABASE_URL and DATABASE_URL both name %q — the suites would truncate the "+
					"database the development server is serving. Use a dedicated test database",
				testName)
		}
	}
	return nil
}

func databaseName(connString string) (string, error) {
	trimmed := strings.TrimSpace(connString)
	if trimmed == "" {
		return "", fmt.Errorf("empty connection string")
	}
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", err
		}
		return strings.TrimPrefix(parsed.Path, "/"), nil
	}
	for _, field := range strings.Fields(trimmed) {
		if name, ok := strings.CutPrefix(field, "dbname="); ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("no database name found in the connection string")
}

func TestTestDatabaseGuard_RefusesDevelopmentAndProductionDatabases(t *testing.T) {
	const devURL = "postgres://localhost:5432/muse_dev?sslmode=disable"

	refused := map[string]string{
		"the development database":    devURL,
		"keyword form":                "host=localhost dbname=muse_dev sslmode=disable",
		"the bare production name":    "postgres://localhost:5432/muse",
		"an explicit production name": "postgres://localhost:5432/muse_production",
		"staging":                     "postgres://localhost:5432/muse_staging",
		"unparseable":                 "not-a-connection-string",
	}
	for name, candidate := range refused {
		if err := refuseNonTestDatabase(candidate, ""); err == nil {
			t.Errorf("%s (%q) was accepted; it must be refused", name, candidate)
		}
	}

	accepted := []string{
		"postgres://localhost:5432/muse_test?sslmode=disable",
		"postgres://localhost:5432/muse_ci",
		"host=localhost dbname=muse_test",
	}
	for _, candidate := range accepted {
		if err := refuseNonTestDatabase(candidate, devURL); err != nil {
			t.Errorf("%q should be accepted: %v", candidate, err)
		}
	}

	custom := "postgres://localhost:5432/my_local_muse"
	if err := refuseNonTestDatabase(custom, custom); err == nil {
		t.Error("a test URL equal to DATABASE_URL must be refused whatever the database is called")
	}
	if err := refuseNonTestDatabase(custom, ""); err != nil {
		t.Errorf("the same URL with no DATABASE_URL set should be accepted: %v", err)
	}
}
