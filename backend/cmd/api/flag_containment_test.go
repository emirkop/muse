package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const flagPackagePath = "muse-backend/internal/platform/featureflag"

var flagReaders = map[string]string{
	"featureflag.VisitorAudibleRoomMusic": "cmd/api/main.go",
}

var importsAllowedIn = map[string]bool{
	"cmd/api": true,
}

func TestFlagsStayContainedInTheWiringLayer(t *testing.T) {
	root := filepath.Join("..", "..")
	patterns := []string{
		filepath.Join(root, "cmd", "*", "*.go"),
		filepath.Join(root, "internal", "*", "*.go"),
		filepath.Join(root, "internal", "*", "*", "*.go"),
	}

	var files []string
	for _, pattern := range patterns {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		files = append(files, matched...)
	}
	if len(files) == 0 {
		t.Fatal("matched no Go files — the check is broken, not the tree")
	}

	importers := map[string][]string{}
	readerCounts := map[string]map[string]int{}
	for identifier := range flagReaders {
		readerCounts[identifier] = map[string]int{}
	}

	scanned := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		scanned++
		text := string(source)
		relative := strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(root)+"/")
		packageDir := filepath.ToSlash(filepath.Dir(relative))

		if strings.Contains(text, flagPackagePath) && packageDir != "internal/platform/featureflag" {
			importers[packageDir] = append(importers[packageDir], filepath.Base(path))
		}
		for identifier := range flagReaders {
			if count := strings.Count(text, identifier); count > 0 {
				readerCounts[identifier][relative] += count
			}
		}
	}
	if scanned == 0 {
		t.Fatal("read no production Go files — the check is broken, not the tree")
	}
	t.Logf("scanned %d production Go files", scanned)

	for packageDir, inFiles := range importers {
		if importsAllowedIn[packageDir] {
			continue
		}
		t.Errorf("%s imports the feature flag package (%v). Flags are resolved in cmd/api and passed "+
			"onwards as plain values; a bounded context branching on a flag makes the flag permanent "+
			"and its removal unbounded.", packageDir, inFiles)
	}

	for identifier, expected := range flagReaders {
		found := readerCounts[identifier]
		if len(found) != 1 {
			t.Errorf("%s is read in %d production files (%v); want exactly 1 (%s), so removing the flag "+
				"stays a bounded edit", identifier, len(found), found, expected)
			continue
		}
		for file, count := range found {
			if file != expected {
				t.Errorf("%s is read in %s; expected %s", identifier, file, expected)
			}
			if count != 1 {
				t.Errorf("%s appears %d times in %s; one read, passed onwards as a value, keeps the "+
					"branch in one place (a doc comment naming the qualified identifier counts here — "+
					"own first attempt tripped on exactly that)", identifier, count, file)
			}
		}
	}
}
