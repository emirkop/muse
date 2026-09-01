package infrastructure_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
	"muse-backend/internal/platform/database"
)

func ExternalIdentitiesHasNoEmailColumns(t *testing.T) {
	pool := testPool(t)

	rows, err := pool.Pool().Query(context.Background(),
		`SELECT column_name FROM information_schema.columns
		  WHERE table_name = 'external_identities' ORDER BY column_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(columns)

	want := []string{"account_id", "created_at", "id", "provider", "subject"}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("external_identities columns = %v, want exactly %v — a provider identity is (provider, subject) and holds no address",
			columns, want)
	}
}

func EmailPasswordColumnsAreUntouched(t *testing.T) {
	pool := testPool(t)

	for _, required := range []struct{ table, column, why string }{
		{"password_credentials", "email", "the login identifier, read by FindByEmail on every email log-in"},
		{"pending_signups", "email", "verify-first sign-up"},
		{"email_outbox", "email", "the delivery address (transient, )"},
		{"password_credentials", "password_hash", "Argon2id"},
	} {
		var exists bool
		if err := pool.Pool().QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns
			                WHERE table_name = $1 AND column_name = $2)`,
			required.table, required.column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("%s.%s is REQUIRED (%s) and must not have been dropped alongside the provider email",
				required.table, required.column, required.why)
		}
	}
}

func LinkedIdentityPersistsProviderAndSubjectOnly_AndResolvesBySubject(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	const relay = "zzq9plural@privaterelay.appleid.com"

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{DisplayName: "provider user"},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-provider-subject"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}

	found, err := repo.FindByLinkedIdentity(ctx, domain.ProviderApple, "apple-provider-subject")
	if err != nil {
		t.Fatalf("FindByLinkedIdentity: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("resolution must return the same account: %q vs %q", found.ID, created.ID)
	}

	if _, err := repo.FindByLinkedIdentity(ctx, domain.ProviderApple, "some-other-subject"); err == nil {
		t.Fatal("an unknown subject must not resolve")
	}

	if tables := findTextInEveryTable(t, pool, relay); len(tables) > 0 {
		t.Fatalf("the provider's address was persisted in %v", tables)
	}
	if tables := findTextInEveryTable(t, pool, "privaterelay"); len(tables) > 0 {
		t.Fatalf("a relay-address fragment was persisted in %v", tables)
	}
}

func NoEmailLinkingTargetExistsOnAProviderIdentity(t *testing.T) {
	pool := testPool(t)

	rows, err := pool.Pool().Query(context.Background(),
		`SELECT column_name FROM information_schema.columns
		  WHERE table_name = 'external_identities'
		    AND data_type IN ('text', 'character varying')
		    AND column_name NOT IN ('provider', 'subject')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var unexpected []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		unexpected = append(unexpected, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(unexpected) > 0 {
		t.Fatalf("external_identities carries text column(s) %v besides provider/subject — each is a potential email-linking JOIN target", unexpected)
	}
}

func findTextInEveryTable(t *testing.T, pool *database.Pool, needle string) []string {
	t.Helper()
	ctx := context.Background()

	rows, err := pool.Pool().Query(ctx,
		`SELECT table_name, column_name FROM information_schema.columns
		  WHERE table_schema = 'public' AND data_type IN ('text', 'character varying')
		  ORDER BY table_name, column_name`)
	if err != nil {
		t.Fatal(err)
	}
	type target struct{ table, column string }
	var targets []target
	for rows.Next() {
		var tbl, col string
		if err := rows.Scan(&tbl, &col); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		targets = append(targets, target{tbl, col})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(targets) < 10 {
		t.Fatalf("the scan found only %d text columns — it is broken, not the schema", len(targets))
	}

	found := map[string]bool{}
	for _, tgt := range targets {
		var hit bool
		query := `SELECT EXISTS(SELECT 1 FROM public.` + pgIdent(tgt.table) +
			` WHERE ` + pgIdent(tgt.column) + ` LIKE '%' || $1 || '%')`
		if err := pool.Pool().QueryRow(ctx, query, needle).Scan(&hit); err != nil {
			t.Fatalf("scanning %s.%s: %v", tgt.table, tgt.column, err)
		}
		if hit {
			found[tgt.table] = true
		}
	}

	tables := make([]string, 0, len(found))
	for name := range found {
		tables = append(tables, name)
	}
	sort.Strings(tables)
	return tables
}

func pgIdent(name string) string {
	if strings.ContainsAny(name, `"\`) {
		panic("unexpected identifier from information_schema: " + name)
	}
	return `"` + name + `"`
}
