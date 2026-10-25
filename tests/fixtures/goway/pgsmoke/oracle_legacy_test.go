package pgsmoke_test

import (
	old "foundry.test/goway/oldmodels/mutators"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"testing"
)

// Retire the legacy read after the integrated-tree freeze. The behavior test
// remains independent of generated models; this test owns only oracle capture.
func TestMutatorHydrationParity(t *testing.T) {
	db, current, _ := insertMutatorFixture(t)
	legacy, err := old.QueryMutatorMembers().RequireFind(t.Context(), db, current.ID)
	must(t, err)
	if !oracleEqual(t, "goway/pgsmoke/mutators/hydration", oracle.Snapshot(legacy.Email), oracle.Snapshot(current.Email)) {
		t.Fatal("persisted mutator hydration changed")
	}
}
