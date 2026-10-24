package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"testing"
)

func TestConflictBridgesRejectAbsentCapabilities(t *testing.T) {
	if FoundryConflictData(Conflict[transformRow]{}, sqlowner.ExpressionSeal{}).Err == nil {
		t.Fatal("untrusted policy read")
	}
	if FoundryConflict[transformRow](ormcore.ConflictPolicy{}, sqlowner.ExpressionSeal{}).state.err == nil {
		t.Fatal("untrusted policy construction")
	}
	if FoundryConflictUpdate[transformRow](ormcore.ConflictAssignment{}, sqlowner.ExpressionSeal{}).state.err == nil {
		t.Fatal("untrusted assignment construction")
	}
	if FoundryConflict[transformRow](ormcore.ConflictPolicy{Owner: "application"}, sqlowner.Expressions()).state.err == nil {
		t.Fatal("foreign policy owner")
	}
	if FoundryConflictUpdate[transformRow](ormcore.ConflictAssignment{Owner: "application"}, sqlowner.Expressions()).state.err == nil {
		t.Fatal("foreign destination owner")
	}
}
