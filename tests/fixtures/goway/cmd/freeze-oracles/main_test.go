package main

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplayGroupsRequirePostgresOnlyInFullCapture(t *testing.T) {
	fixture, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, postgres := range []bool{false, true} {
		groups, sites, err := replayGroups(fixture, postgres)
		if err != nil {
			t.Fatal(err)
		}
		if sites["consumer/models/postgres/hydration"] != postgres {
			t.Fatal("incorrect PostgreSQL selection")
		}
		if !sites["goway/integrations/pagination/parse"] {
			t.Fatal("cursor parsing absent")
		}
		found := false
		for _, g := range groups {
			if filepath.Base(g.dir) == "models" {
				found = true
				if strings.Contains(strings.Join(g.args, " "), "TestPostgresSharedDeclarationRelationParity") != postgres {
					t.Fatal(g)
				}
			}
		}
		if !found {
			t.Fatal("model comparisons absent")
		}
	}
}

func TestInventoryRequiresUniqueSitesAndFindsTestOwners(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package sample\nfunc TestActual(t *testing.T) { oracleEqual(t, \"one\", a, b) }\n")
	if err := os.WriteFile(filepath.Join(dir, "one_test.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	sites, tests, err := oracle.Inventory(dir)
	if err != nil || !sites["one"] || len(tests) != 1 || tests[0] != "TestActual" {
		t.Fatal(sites, tests, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "two_test.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = oracle.Inventory(dir); err == nil {
		t.Fatal("duplicate oracle sites accepted")
	}
}

func TestInventoryUsesSyntaxAndRejectsDynamicSite(t *testing.T) {
	dir := t.TempDir()
	source := "package sample\nvar quoted = `oracleEqual(t, \"fake\", a, b)`\nfunc TestActual(t *testing.T) { oracleEqual(\nt, \"real\", a,b) }\n"
	path := filepath.Join(dir, "one_test.go")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	sites, _, err := oracle.Inventory(dir)
	if err != nil || len(sites) != 1 || !sites["real"] {
		t.Fatal(sites, err)
	}
	source = "package sample\nfunc TestActual(t *testing.T) { oracleEqual(t, variable, a,b) }\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = oracle.Inventory(dir); err == nil {
		t.Fatal("dynamic site accepted")
	}
}

func TestRefreezeCaptureHasIsolatedPendingInventory(t *testing.T) {
	stage := t.TempDir()
	env, err := prepareCapture(stage)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 3 || env[1] != "FOUNDRY_ORACLE_READ="+stage {
		t.Fatal(env)
	}
	b, err := oracle.Load(stage)
	if err != nil || len(b.Records) != 0 {
		t.Fatal(b, err)
	}
	// Added/removed sites are discovered against pending capture, not the
	// published site set. The replacement still must pass ordinary validation.
	path := filepath.Join(stage, "sites_test.go")
	if err := os.WriteFile(path, []byte("package fixture\nfunc TestNew(t *testing.T){ oracleEqual(t, \"new\",a,b) }"), 0600); err != nil {
		t.Fatal(err)
	}
	sites, _, err := oracle.Inventory(stage)
	if err != nil || !sites["new"] {
		t.Fatal(sites, err)
	}
}

func TestInventoryFollowsCrossFileHelpers(t *testing.T) {
	dir := t.TempDir()
	sources := map[string]string{
		"helper_test.go": "package fixture\nfunc compare(t *testing.T){oracleEqual(t,\"helper\",a,b)}",
		"caller_test.go": "package fixture\nfunc TestPaired(t *testing.T){compare(t)}\nfunc TestUnpaired(t *testing.T){}",
	}
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, tests, err := oracle.Inventory(dir)
	if err != nil || len(tests) != 1 || tests[0] != "TestPaired" {
		t.Fatal(tests, err)
	}
}
