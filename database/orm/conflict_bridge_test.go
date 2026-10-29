package orm_test

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"testing"
)

func TestConflictBridgesRejectAbsentCapabilities(t *testing.T) {
	if orm.FoundryConflictData(orm.Conflict[transformRow]{}, sqlowner.ExpressionSeal{}).Err == nil {
		t.Fatal("untrusted policy read")
	}
	if orm.ConflictError(orm.FoundryConflict[transformRow](ormcore.ConflictPolicy{}, sqlowner.ExpressionSeal{})) == nil {
		t.Fatal("untrusted policy construction")
	}
	if orm.ConflictUpdateError(orm.FoundryConflictUpdate[transformRow](ormcore.ConflictAssignment{}, sqlowner.ExpressionSeal{})) == nil {
		t.Fatal("untrusted assignment construction")
	}
	if orm.ConflictError(orm.FoundryConflict[transformRow](ormcore.ConflictPolicy{Owner: "application"}, sqlowner.Expressions())) == nil {
		t.Fatal("foreign policy owner")
	}
	if orm.ConflictUpdateError(orm.FoundryConflictUpdate[transformRow](ormcore.ConflictAssignment{Owner: "application"}, sqlowner.Expressions())) == nil {
		t.Fatal("foreign destination owner")
	}
}
