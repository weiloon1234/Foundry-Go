package oracle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentCensusRejectsUninstrumentedNewSources(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"consumer", "goway"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "consumer/parity_test.go")
	body := []byte("package sample\nfunc TestParity(t *testing.T){ if old!=current {t.Fatal(\"drift\")} }\n")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	audit := ComparisonAudit{Files: map[string]AuditedSource{"consumer/parity_test.go": {SHA256: Digest(body), Tests: []string{"TestParity"}}}}
	if err := audit.Validate(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "consumer/missed_test.go"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := audit.Validate(root); err == nil {
		t.Fatal("new uninstrumented comparison missed")
	}
}

func TestSelectedSitesKeepsSharedHelpersAndSeparatesPostgres(t *testing.T) {
	dir := t.TempDir()
	body := `package example
func compare(t *testing.T){oracleEqual(t,"shared",a,b)}
func TestUnit(t *testing.T){compare(t);oracleEqual(t,"unit",a,b)}
func TestPostgres(t *testing.T){compare(t);oracleEqual(t,"postgres",a,b)}
`
	if err := os.WriteFile(filepath.Join(dir, "parity_test.go"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	sites, err := SelectedSites(dir, []string{"TestUnit"})
	if err != nil || len(sites) != 2 || !sites["shared"] || !sites["unit"] || sites["postgres"] {
		t.Fatal(sites, err)
	}
	sites, err = SelectedSites(dir, []string{"TestPostgres"})
	if err != nil || len(sites) != 2 || !sites["shared"] || !sites["postgres"] || sites["unit"] {
		t.Fatal(sites, err)
	}
}

func TestOfflineCaptureOnlyOmitsDeclaredPostgresTests(t *testing.T) {
	audit := ComparisonAudit{Files: map[string]AuditedSource{"p/parity_test.go": {Tests: []string{"TestUnit", "TestPG"}, PostgresTests: []string{"TestPG"}}}}
	record := Record{Package: "p", Kind: "golden", Site: "shared", Transcript: "observed"}
	unit, pg := "p/TestUnit/shared/0", "p/TestPG/subtest/shared/0"
	previous := Bundle{Records: map[string]Record{unit: record, pg: record}}
	captured := Bundle{Records: map[string]Record{unit: record}}
	if err := audit.CompareCapture(previous, captured, false); err != nil {
		t.Fatal(err)
	}
	if err := audit.CompareCapture(previous, captured, true); err == nil {
		t.Fatal("full capture omitted PostgreSQL")
	}
	if err := audit.CompareCapture(previous, Bundle{}, false); err == nil {
		t.Fatal("offline capture hid a deleted ordinary test")
	}
	if err := (ComparisonAudit{}).CompareCapture(previous, captured, false); err == nil {
		t.Fatal("an unregistered PostgreSQL test was silently omitted")
	}
	changed := record
	changed.Transcript = "changed"
	if err := audit.CompareCapture(previous, Bundle{Records: map[string]Record{unit: changed}}, false); err == nil {
		t.Fatal("changed ordinary observation accepted")
	}
	if err := audit.CompareCapture(captured, previous, false); err == nil {
		t.Fatal("added observation accepted")
	}
}
