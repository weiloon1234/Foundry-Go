package goway_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"os"
	"testing"
)

var matchingOracle = oracle.NewReplayAt("goway", "../../oracle/testdata/stage8d-matching")

var frozenOracle = oracle.NewReplay("goway")

func TestMain(m *testing.M) { os.Exit(matchingOracle.Finish(frozenOracle.Main(m))) }
func oracleEqual(t *testing.T, site, current string) bool {
	t.Helper()
	return frozenOracle.Equal(t, site, current)
}
func TestFrozenOracleIntegrity(t *testing.T) {
	b, err := oracle.Load(oracleReadDirectory())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) > 0 {
		sites, _, err := oracle.Inventory(".")
		if err != nil {
			t.Fatal(err)
		}
		observed := map[string]bool{}
		for _, r := range b.Records {
			if r.Kind == "golden" && r.Package == "goway" {
				observed[r.Site] = true
			}
		}
		for site := range sites {
			if !observed[site] {
				t.Errorf("missing frozen site %s", site)
			}
		}
		for site := range observed {
			if !sites[site] {
				t.Errorf("frozen comparison site removed: %s", site)
			}
		}
	}
	if len(b.Records) == 0 {
		t.Fatal("published frozen oracle is required")
	}
}

func oracleReadDirectory() string { return oracle.Directory() }

func oracleMatch(t *testing.T, site, current string) bool {
	t.Helper()
	return frozenOracle.Match(t, site, current)
}
