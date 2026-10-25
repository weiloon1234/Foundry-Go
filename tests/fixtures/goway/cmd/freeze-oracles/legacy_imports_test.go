package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

func TestLegacyImportsStayInRecordingPaths(t *testing.T) {
	fixture, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var audit oracle.ComparisonAudit
	if err := oracle.ReadJSON(filepath.Join(fixture, "testdata/oracles/comparisons.json"), &audit); err != nil {
		t.Fatal(err)
	}
	importers, err := recordingImporters(fixture, audit)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(importers))
	for file := range importers {
		files = append(files, file)
	}
	slices.Sort(files)
	for _, file := range files {
		t.Logf("%s: %s", file, strings.Join(importers[file], ", "))
	}
	t.Logf("remaining legacy model importer files: %d", len(files))
}

func TestLegacyImportBoundaryUsesImportsAndRejectsBehaviorDependencies(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "tests/fixtures/goway")
	for _, dir := range []string{"oldmodels", "oldreporting"} {
		if err := os.MkdirAll(filepath.Join(fixture, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	metadata := filepath.Join(fixture, "metadata.go")
	if err := os.WriteFile(metadata, []byte("package fixture\nvar pkg = `foundry.test/goway/oldmodels`\n"), 0600); err != nil {
		t.Fatal(err)
	}
	behavior := filepath.Join(fixture, "behavior_test.go")
	if err := os.WriteFile(behavior, []byte("package fixture\nimport legacy \"foundry.test/goway/oldmodels/mutators\"\nfunc TestRecorded(t *testing.T) { _ = legacy.Member{} }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	importers, err := legacyModelImporters(root)
	if err != nil || len(importers) != 1 || len(importers["tests/fixtures/goway/behavior_test.go"]) != 1 {
		t.Fatal(importers, err)
	}
	audit := oracle.ComparisonAudit{}
	if _, err := recordingImporters(fixture, audit); err == nil || !strings.Contains(err.Error(), "behavior_test.go") {
		t.Fatal("unaudited behavior dependency accepted", err)
	}
	audit.Files = map[string]oracle.AuditedSource{"goway/behavior_test.go": {Tests: []string{"TestRecorded"}}}
	if _, err := recordingImporters(fixture, audit); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyImportBoundaryRejectsOrdinaryTestInRecordingFile(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "tests/fixtures/goway")
	for _, dir := range []string{"oldmodels", "oldreporting"} {
		if err := os.MkdirAll(filepath.Join(fixture, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(fixture, "paired_test.go")
	source := `package fixture
import old "foundry.test/goway/oldmodels"
func TestRecorded(t *testing.T) { _ = old.User{} }
func TestOrdinary(t *testing.T) { _ = old.User{} }
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	audit := oracle.ComparisonAudit{Files: map[string]oracle.AuditedSource{"goway/paired_test.go": {Tests: []string{"TestRecorded"}}}}
	if _, err := recordingImporters(fixture, audit); err == nil || !strings.Contains(err.Error(), "TestOrdinary") {
		t.Fatal("ordinary test hidden behind recorded entry point", err)
	}
}

func TestLegacyImportBoundaryParsesEscapedImportPaths(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "tests/fixtures/goway")
	for _, dir := range []string{"oldmodels", "oldreporting"} {
		if err := os.MkdirAll(filepath.Join(fixture, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(fixture, "behavior_test.go")
	source := `package fixture
import old "foundry.test/goway/\x6fldmodels"
func TestOrdinary(t *testing.T) { _ = old.User{} }
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	importers, err := legacyModelImporters(root)
	if err != nil || !slices.Equal(importers["tests/fixtures/goway/behavior_test.go"], []string{"foundry.test/goway/oldmodels"}) {
		t.Fatal(importers, err)
	}
	if _, err := recordingImporters(fixture, oracle.ComparisonAudit{}); err == nil || !strings.Contains(err.Error(), "behavior_test.go") {
		t.Fatal("escaped import bypassed recording boundary", err)
	}
}

func TestMigrationBeforeExceptionIsLimitedToNamedFiles(t *testing.T) {
	for _, tc := range []struct {
		file    string
		allowed bool
	}{
		{"migration/guide_test.go", true},
		{"migration/examples_test.go", true},
		{"migration/sql_test.go", true},
		{"migration/ordinary_test.go", false},
		{"migration/examples_extra_test.go", false},
		{"migration/nested/examples_test.go", false},
		{"elsewhere/guide_test.go", false},
		{"elsewhere/examples_test.go", false},
		{"elsewhere/sql_test.go", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join(root, "tests/fixtures/goway")
			for _, dir := range []string{"oldmodels", "oldreporting", filepath.Dir(tc.file)} {
				if err := os.MkdirAll(filepath.Join(fixture, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(fixture, tc.file)
			source := `package fixture
import old "foundry.test/goway/oldmodels"
func TestBeforeExample(t *testing.T) { _ = old.User{} }
`
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := recordingImporters(fixture, oracle.ComparisonAudit{})
			if tc.allowed && err != nil {
				t.Fatal("migration-before example rejected", err)
			}
			if !tc.allowed && (err == nil || !strings.Contains(err.Error(), tc.file)) {
				t.Fatal("unrelated file borrowed migration-before exception", err)
			}
		})
	}
}
