package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	collectionPkg = "internal/collection"
	museumPkg     = "internal/museum"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

func goFilesUnder(t *testing.T, root, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no Go files under %s — this test is broken, not the tree", dir)
	}
	return files
}

func TestNoImportBetweenCollectionAndMuseum(t *testing.T) {
	root := repoRoot(t)

	check := func(dir, forbidden string) {
		for _, file := range goFilesUnder(t, root, dir) {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			for _, spec := range parsed.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("unquote %s in %s: %v", spec.Path.Value, file, err)
				}
				if strings.Contains(path, forbidden) {
					rel, _ := filepath.Rel(root, file)
					t.Errorf("%s imports %q — `01` §5.1 requires the Museum and Collection trees to be independent; "+
						"state the capability you need as a port and let cmd/api wire it (backend/ Dependency Rule #2)",
						rel, path)
				}
			}
		}
	}

	check(collectionPkg, "muse-backend/"+museumPkg)
	check(museumPkg, "muse-backend/"+collectionPkg)
}

func declaredTypeNames(t *testing.T, root, dir string) map[string]string {
	t.Helper()
	names := map[string]string{}
	for _, file := range goFilesUnder(t, root, dir) {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			generic, ok := decl.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, spec := range generic.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || !typeSpec.Name.IsExported() {
					continue
				}
				rel, _ := filepath.Rel(root, file)
				names[typeSpec.Name.Name] = rel
			}
		}
	}
	return names
}

func TestNoSharedDomainTypeNames(t *testing.T) {
	root := repoRoot(t)

	collection := declaredTypeNames(t, root, collectionPkg+"/domain")
	museum := declaredTypeNames(t, root, museumPkg+"/domain")

	var collisions []string
	for name, collectionFile := range collection {
		if museumFile, clash := museum[name]; clash {
			collisions = append(collisions, fmt.Sprintf("%s (%s and %s)", name, collectionFile, museumFile))
		}
	}
	sort.Strings(collisions)
	if len(collisions) > 0 {
		t.Errorf("type name(s) declared in both domains: %s\n"+
			"`01` §5.1 forbids modelling a Collection Room as a Room type, and `04`'s Risk Register warns against "+
			"unifying the two trees into one \"space with items\" abstraction. If the concepts really are the same, "+
			"that is a product decision, not a refactor.", strings.Join(collisions, ", "))
	}

	if len(collection) == 0 || len(museum) == 0 {
		t.Fatalf("scanned %d collection types and %d museum types — the scan is broken, not the tree",
			len(collection), len(museum))
	}
}

func tablesNamedIn(t *testing.T, root, dir string, candidates []string) []string {
	t.Helper()
	found := map[string]bool{}
	for _, file := range goFilesUnder(t, root, dir) {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				text = literal.Value
			}
			if !looksLikeSQL(text) {
				return true
			}
			for _, table := range candidates {
				if namesTable(text, table) {
					found[table] = true
				}
			}
			return true
		})
	}
	out := make([]string, 0, len(found))
	for table := range found {
		out = append(out, table)
	}
	sort.Strings(out)
	return out
}

func looksLikeSQL(text string) bool {
	lowered := strings.ToLower(text)
	for _, keyword := range []string{
		"select ", "insert into ", "update ", "delete from ",
		"join ", "from ", "references ", "truncate ", "alter table ", "create table ",
	} {
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
}

func namesTable(text, table string) bool {
	isIdentifierByte := func(b byte) bool {
		return b == '_' ||
			(b >= 'a' && b <= 'z') ||
			(b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9')
	}
	for offset := 0; ; {
		index := strings.Index(text[offset:], table)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(table)
		beforeOK := start == 0 || !isIdentifierByte(text[start-1])
		afterOK := end == len(text) || !isIdentifierByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
	}
}

func TestNoTableIsNamedByBothPackages(t *testing.T) {
	root := repoRoot(t)

	museumTables := []string{"museums", "rooms", "room_photo_slots", "room_sculptures"}
	collectionTables := []string{"collection_rooms", "collection_items"}

	if leaked := tablesNamedIn(t, root, collectionPkg, museumTables); len(leaked) > 0 {
		t.Errorf("internal/collection names Museum table(s) %v — the two trees share no table (`01` §5.1)", leaked)
	}
	if leaked := tablesNamedIn(t, root, museumPkg, collectionTables); len(leaked) > 0 {
		t.Errorf("internal/museum names Collection table(s) %v — the two trees share no table (`01` §5.1)", leaked)
	}

	if own := tablesNamedIn(t, root, collectionPkg, collectionTables); len(own) != len(collectionTables) {
		t.Fatalf("the scanner found only %v of the Collection tables — it is broken, so the clean results above prove nothing", own)
	}
	if own := tablesNamedIn(t, root, museumPkg, museumTables); len(own) != len(museumTables) {
		t.Fatalf("the scanner found only %v of the Museum tables — it is broken, so the clean results above prove nothing", own)
	}
}

func TestNeitherTreeCarriesTheOthersIdentifier(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	forbidden := map[string][]string{
		"collection_rooms": {"museum_id", "room_id", "style_id", "variant_id"},
		"collection_items": {"museum_id", "room_id", "photo_asset_id"},
		"museums":          {"collection_room_id", "collection_item_id"},
		"rooms":            {"collection_room_id", "collection_item_id"},
		"room_photo_slots": {"collection_room_id", "collection_item_id"},
		"room_sculptures":  {"collection_room_id", "collection_item_id"},
	}

	for table, columns := range forbidden {
		for _, column := range columns {
			var exists bool
			err := s.pool.Pool().QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_name = $1 AND column_name = $2
				)
			`, table, column).Scan(&exists)
			if err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Errorf("%s.%s exists — the Museum and Collection trees must not reference each other (`01` §5.1); they meet only at accounts", table, column)
			}
		}
	}

	var ownedByAccount bool
	err := s.pool.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'collection_rooms' AND column_name = 'account_id'
		)
	`).Scan(&ownedByAccount)
	if err != nil {
		t.Fatal(err)
	}
	if !ownedByAccount {
		t.Fatal("collection_rooms has no account_id — a Collection Room is owned directly by the User (`04` Collection Room Data)")
	}
}
