package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func TestReportReorderingPreservesMaterializedWinnerSelection(t *testing.T) {
	m, r := declareMembers(), declareRows()
	base := rows.Select(rows.From(m.Query().Where(orm.NumberKeyGt(orm.NumberKey(m.Key()), 2))), r, rows.Map(r.ID, expr.Value(orm.KeyExpr(m.ID))), rows.Map(r.Name, expr.Value(orm.TextExpr(m.Name))))
	winners := rows.Limit(rows.OrderBy(rows.DistinctOn(base, expr.Group(expr.Value(orm.KeyExpr(m.ID)))), expr.Desc(expr.Value(orm.KeyExpr(m.ID)))), 5)
	source := rows.AsRecord[left](rows.RecordOf(winners), "winners")
	id := rows.At(rows.Scope(source), r.ID)
	name := rows.At(rows.Scope(source), r.Name)
	report := rows.OrderBy(rows.Select(source, r, rows.MapSelected(r.ID, expr.AsSelected(id)), rows.MapSelected(r.Name, expr.AsSelected(name))), expr.Desc(id))
	statement, err := rows.CompileRead(rows.Read(rows.ReorderUnwindowed(report, expr.Asc(id))))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`DISTINCT ON ("members"."id")`, `ORDER BY "members"."id" DESC LIMIT $2) AS "winners"`, `ORDER BY "winners"."id" ASC`} {
		if !strings.Contains(statement.SQL(), fragment) {
			t.Fatal("materialized winner changed", statement.SQL())
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{int64(2), int64(5)}) {
		t.Fatal(statement.Arguments())
	}
	original, err := rows.CompileRead(rows.Read(report))
	if err != nil || !strings.HasSuffix(original.SQL(), `ORDER BY "winners"."id" DESC`) {
		t.Fatal("outer derivation mutated original", original.SQL(), err)
	}
}

func TestReportReorderingReplacesOnlyOuterOrders(t *testing.T) {
	m, r := declareMembers(), declareRows()
	id := expr.Value(orm.KeyExpr(m.ID))
	report := rows.OrderBy(rows.Select(rows.From(m.Query().Where(orm.NumberKeyGt(orm.NumberKey(m.Key()), 4))), r, rows.Map(r.ID, id), rows.Map(r.Name, expr.Value(orm.TextExpr(m.Name)))), expr.Desc(id))
	before, err := rows.CompileRead(rows.Read(report))
	if err != nil {
		t.Fatal(err)
	}
	after, err := rows.CompileRead(rows.Read(rows.ReorderUnwindowed(report, expr.Asc(id))))
	if err != nil || !strings.HasSuffix(after.SQL(), `ORDER BY "members"."id" ASC`) || !reflect.DeepEqual(after.Arguments(), []any{int64(4)}) {
		t.Fatal(after.SQL(), after.Arguments(), err)
	}
	again, _ := rows.CompileRead(rows.Read(report))
	if before.SQL() != again.SQL() || !reflect.DeepEqual(before.Arguments(), again.Arguments()) {
		t.Fatal("reorder mutated source")
	}
	for name, bad := range map[string]rows.Query[member, record]{"limit": rows.Limit(report, 1), "zero limit": rows.Limit(report, 0), "offset": rows.Offset(report, 1), "distinct on": rows.DistinctOn(report, expr.Group(id)), "zero": {}} {
		if s, e := rows.CompileRead(rows.Read(rows.ReorderUnwindowed(bad, expr.Asc(id)))); !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("unsafe report", name, e)
		}
	}
	if _, err := rows.CompileRead(rows.Read(rows.ReorderUnwindowed(report))); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty order", err)
	}
	if s, err := rows.CompileRead(rows.Read(rows.ReorderUnwindowed(rows.Distinct(report), expr.Asc(id)))); err != nil || !strings.HasPrefix(s.SQL(), "SELECT DISTINCT ") {
		t.Fatal("lost DISTINCT", s.SQL(), err)
	}
}
