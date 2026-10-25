package correlations_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"os"
	"testing"
)

var frozenOracle = oracle.NewReplay("consumer/correlations")

func TestMain(m *testing.M) { os.Exit(frozenOracle.Main(m)) }
func oracleEqual(t *testing.T, site, legacy, current string) bool {
	t.Helper()
	return frozenOracle.Equal(t, site, legacy, current)
}
