package oracle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedSitesKeepsSharedHelpersAndSeparatesPostgres(t *testing.T) {
	dir := t.TempDir()
	body := `package example
func compare(t *testing.T){oracleEqual(t,"shared",b)}
func TestUnit(t *testing.T){compare(t);oracleEqual(t,"unit",b)}
func TestPostgres(t *testing.T){compare(t);oracleEqual(t,"postgres",b)}
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
