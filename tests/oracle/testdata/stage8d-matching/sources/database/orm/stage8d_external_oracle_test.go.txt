package orm_test

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"os"
	"testing"
)

var frameworkOracle = oracle.NewReplayAt("orm_external", "../../tests/oracle/testdata/stage8d")

func TestMain(m *testing.M) { os.Exit(frameworkOracle.Finish(orm.Stage8dOracleFinish(m.Run()))) }
func frozenMatch(t *testing.T, site string, current any) {
	t.Helper()
	if !frameworkOracle.Match(t, site, oracle.Snapshot(current)) {
		t.Fatal("legacy observation changed")
	}
}
