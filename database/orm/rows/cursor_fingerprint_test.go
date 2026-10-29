package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
	fingerprint := func(q rows.Record[aggregateRecord], both bool) string {
		c := rows.CursorFor(q)
		id := expr.At(c.Scope(), orm.KeyExpr(d.Key()))
		keys := []expr.GroupKey[rows.CursorScope[aggregateRecord]]{expr.Group(id)}
		if both {
			keys = append(keys, expr.Group(expr.At(c.Scope(), orm.NumberExpr(d.Exact))))
		}
		plan, err := ormcore.PlanResultCursor(t.Context(), rows.CursorQueryCore(c.UniqueBy(keys...)), 2, "", false, false)
		if err != nil {
			t.Fatal(err)
		}
		return plan.Scope
	}
	want := fingerprint(rows.ModelRecord(base), false)
	for _, changed := range []rows.Record[aggregateRecord]{rows.ModelRecord(base.Where(orm.NumberGt(d.Exact, 1))), rows.ModelRecord(base.OrderBy(orm.NumberAsc(d.Exact))), rows.ModelRecord(base.Limit(1)), rows.ModelRecord(base.Offset(1))} {
		if fingerprint(changed, false) == want {
			t.Fatal("changed input retained cursor fingerprint")
		}
	}
	if fingerprint(rows.ModelRecord(base), true) == want {
		t.Fatal("identity omitted from fingerprint")
	}
	c := rows.CursorFor(rows.ModelRecord(base))
	c = c.UniqueBy(expr.Group(expr.At(c.Scope(), orm.KeyExpr(d.Key()))))
	statement, err := ormcore.CompileResultCursor(t.Context(), rows.CursorQueryCore(c))
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeFor[aggregateRecord]()
	unscoped, err := querycore.CursorStatementScopeFor(typ.PkgPath()+"/"+typ.String(), statement, nil)
	if err != nil || unscoped == want {
		t.Fatal("result namespace omitted", err)
	}
}
