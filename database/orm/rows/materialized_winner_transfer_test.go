package rows

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
	base := Select(From(m.Query().Where(orm.NumberKeyGt(orm.NumberKey(m.Key()), 2))), r, Map(r.ID, expr.Value(orm.KeyExpr(m.ID))), Map(r.Name, expr.Value(orm.TextExpr(m.Name))))
	winners := Limit(OrderBy(DistinctOn(base, expr.Group(expr.Value(orm.KeyExpr(m.ID)))), expr.Value(orm.KeyExpr(m.ID)).Desc()), 5)
	source := AsRecord[left](RecordOf(winners), "winners")
	id := At(Scope(source), r.ID)
	name := At(Scope(source), r.Name)
	report := OrderBy(Select(source, r, MapSelected(r.ID, id.Value()), MapSelected(r.Name, name.Value())), id.Desc())
	statement, err := CompileRead(Read(ReorderUnwindowed(report, id.Asc())))
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
	original, err := CompileRead(Read(report))
	if err != nil || !strings.HasSuffix(original.SQL(), `ORDER BY "winners"."id" DESC`) {
		t.Fatal("outer derivation mutated original", original.SQL(), err)
	}
}

func TestReportReorderingReplacesOnlyOuterOrders(t *testing.T) {
	m, r := declareMembers(), declareRows()
	id := expr.Value(orm.KeyExpr(m.ID))
	report := OrderBy(Select(From(m.Query().Where(orm.NumberKeyGt(orm.NumberKey(m.Key()), 4))), r, Map(r.ID, id), Map(r.Name, expr.Value(orm.TextExpr(m.Name)))), id.Desc())
	before, err := CompileRead(Read(report))
	if err != nil {
		t.Fatal(err)
	}
	after, err := CompileRead(Read(ReorderUnwindowed(report, id.Asc())))
	if err != nil || !strings.HasSuffix(after.SQL(), `ORDER BY "members"."id" ASC`) || !reflect.DeepEqual(after.Arguments(), []any{int64(4)}) {
		t.Fatal(after.SQL(), after.Arguments(), err)
	}
	again, _ := CompileRead(Read(report))
	if before.SQL() != again.SQL() || !reflect.DeepEqual(before.Arguments(), again.Arguments()) {
		t.Fatal("reorder mutated source")
	}
	for name, bad := range map[string]Query[member, record]{"limit": Limit(report, 1), "zero limit": Limit(report, 0), "offset": Offset(report, 1), "distinct on": DistinctOn(report, expr.Group(id)), "zero": {}} {
		if s, e := CompileRead(Read(ReorderUnwindowed(bad, id.Asc()))); !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("unsafe report", name, e)
		}
	}
	if _, err := CompileRead(Read(ReorderUnwindowed(report))); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty order", err)
	}
	if s, err := CompileRead(Read(ReorderUnwindowed(Distinct(report), id.Asc()))); err != nil || !strings.HasPrefix(s.SQL(), "SELECT DISTINCT ") {
		t.Fatal("lost DISTINCT", s.SQL(), err)
	}
}
