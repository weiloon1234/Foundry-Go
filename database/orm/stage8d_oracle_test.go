package orm

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"testing"
)

var frameworkOracle = oracle.NewReplayAt("orm", "../../tests/oracle/testdata/stage8d")

func Stage8dOracleFinish(code int) int { return frameworkOracle.Finish(code) }
func frozenMatch(t *testing.T, site string, current any) {
	t.Helper()
	if !frameworkOracle.Match(t, site, oracle.Snapshot(current)) {
		t.Fatal("legacy observation changed")
	}
}
