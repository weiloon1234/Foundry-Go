package orm

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestSourceWriteBridgesRejectAbsentCapabilities(t *testing.T) {
	if FoundryInsertMapping[int, int](ormcore.ModelMapping{}, sqlowner.ExpressionSeal{}).core.Err == nil {
		t.Fatal("insert mapping accepted zero capability")
	}
	if FoundryUpdateMapping[int, int](ormcore.ModelMapping{}, sqlowner.ExpressionSeal{}).core.Err == nil {
		t.Fatal("update mapping accepted zero capability")
	}
	if FoundrySourceKey[int, int](ormcore.ModelMapping{}, sqlowner.ExpressionSeal{}).core.Err == nil {
		t.Fatal("source key accepted zero capability")
	}
	q := Query[int]{}
	if FoundryInsertFrom[int](q, ormcore.Source{}, sqlowner.QuerySeal{}).plan.err == nil {
		t.Fatal("insert accepted zero capability")
	}
	if FoundryUpdateFrom[int](q, ormcore.Source{}, sqlowner.QuerySeal{}).plan.err == nil {
		t.Fatal("update accepted zero capability")
	}
	if FoundryDeleteUsing[int](q, ormcore.Source{}, true, SoftDeletable[int]{}, sqlowner.QuerySeal{}).plan.err == nil {
		t.Fatal("delete accepted zero capability")
	}
}
