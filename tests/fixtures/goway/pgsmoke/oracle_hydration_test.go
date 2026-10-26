package pgsmoke_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"testing"
)

// Frozen legacy hydration remains independent of generated models.
func TestMutatorHydrationParity(t *testing.T) {
	_, current, _ := insertMutatorFixture(t)
	if !oracleEqual(t, "goway/pgsmoke/mutators/hydration", oracle.Snapshot(current.Email)) {
		t.Fatal("frozen legacy comparison differs")
	}

}
