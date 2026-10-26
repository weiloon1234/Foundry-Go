package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"reflect"
	"testing"
)

func TestResultCursorFingerprintIncludesInputAndIdentity(t *testing.T) {
	d := declareAggregateRecords()
	base := d.Query()
	fingerprint := func(q Record[aggregateRecord], both bool) string {
		c := CursorFor(q)
		id := expr.At(c.Scope(), d.Key())
		keys := []expr.GroupKey[CursorScope[aggregateRecord]]{expr.Group(id)}
		if both {
			keys = append(keys, expr.Group(expr.At(c.Scope(), d.Exact)))
		}
		plan, err := ormcore.PlanResultCursor(t.Context(), c.UniqueBy(keys...).core, 2, "", false, false)
		if err != nil {
			t.Fatal(err)
		}
		return plan.Scope
	}
	want := fingerprint(ModelRecord(base), false)
	for _, changed := range []Record[aggregateRecord]{ModelRecord(base.Where(d.Exact.Gt(1))), ModelRecord(base.OrderBy(d.Exact.Asc())), ModelRecord(base.Limit(1)), ModelRecord(base.Offset(1))} {
		if fingerprint(changed, false) == want {
			t.Fatal("changed input retained cursor fingerprint")
		}
	}
	if fingerprint(ModelRecord(base), true) == want {
		t.Fatal("identity omitted from fingerprint")
	}
	c := CursorFor(ModelRecord(base))
	c = c.UniqueBy(expr.Group(expr.At(c.Scope(), d.Key())))
	statement, err := ormcore.CompileResultCursor(t.Context(), c.core)
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeFor[aggregateRecord]()
	unscoped, err := querycore.CursorStatementScopeFor(typ.PkgPath()+"/"+typ.String(), statement, nil)
	if err != nil || unscoped == want {
		t.Fatal("result namespace omitted", err)
	}
}
