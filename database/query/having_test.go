package query

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func groupedReport() ProjectionQuery[cursorRecord, reportRecord] {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	return Project(cursorQuery(), reportDefinition(),
		Map(NewProjectionField[reportRecord, int64]("id"), id.Value()),
		Map(NewProjectionField[reportRecord, int64]("count"), Count[cursorRecord]().Value()),
	).GroupBy(id.Group())
}

func TestHavingAndAggregateOrderingShareSelectCompiler(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	values := []int64{1, 2}
	condition := HavingAnd(Count[cursorRecord]().Gt(2), HavingOr(
		id.Sum().Gt(decimal.FromInt64(5)), Grouped(id.In(values...)).Not(),
	))
	q := groupedReport().Where(id.Gt(0)).Having(condition).
		OrderBy(Count[cursorRecord]().Desc(), id.Asc()).Limit(4)
	values[0] = 999
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "records"."id" AS "id", COUNT(*) AS "count" FROM "records" WHERE ("records"."id" > $1) GROUP BY "records"."id" HAVING ((COUNT(*) > $2) AND ((SUM(CAST("records"."id" AS numeric)) > $3) OR (NOT ("records"."id" IN ($4, $5))))) ORDER BY COUNT(*) DESC, "records"."id" ASC LIMIT $6`
	if s.SQL() != want {
		t.Fatal(s.SQL())
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(0), int64(2), "5", int64(1), int64(2), int64(4)}) {
		t.Fatal("WHERE/HAVING/window parameters or captured values changed", s.Arguments())
	}
	derived, err := q.Having(id.Sum().IsNotNull()).OrderBy(id.Sum().Asc()).Compile()
	if err != nil || !strings.Contains(derived.SQL(), "IS NOT NULL") {
		t.Fatal("derived group condition lost", err)
	}
	original, _ := q.Compile()
	if original.SQL() != s.SQL() || !reflect.DeepEqual(original.Arguments(), s.Arguments()) {
		t.Fatal("derivation mutated report")
	}
}

func TestHavingRejectsInvalidScopeStructureAndBindings(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	text := NewTextField[cursorRecord, string]("records", "label", codec.String[string]())
	other := NewExactField[cursorRecord, int64]("other", "id", codec.Signed[int64]())
	floating := NewFloatField[cursorRecord, float64]("records", "id", codec.Float[float64]())
	var absent *expressionOrder[cursorRecord]
	for _, q := range []ProjectionQuery[cursorRecord, reportRecord]{
		groupedReport().Having(HavingPredicate[cursorRecord]{}),
		groupedReport().Having(HavingAnd[cursorRecord]()),
		groupedReport().Having(Grouped(text.Eq("not grouped"))),
		groupedReport().Having(other.Sum().Gt(decimal.FromInt64(1))),
		groupedReport().Having(floating.Avg().Gt(math.Inf(1))),
		groupedReport().OrderBy(nil), groupedReport().OrderBy(absent),
		groupedReport().OrderBy(Expression[cursorRecord, int64]{}.Desc()),
		groupedReport().OrderBy(other.Sum().Asc()),
		groupedReport().Where(Predicate[cursorRecord]{expression: id.Sum().Gt(decimal.FromInt64(0)).expression}),
	} {
		if _, err := q.Compile(); err == nil {
			t.Fatal("invalid group condition/order accepted")
		}
	}
	// Even a privately forged model predicate cannot smuggle an aggregate into WHERE.
	forged := Predicate[cursorRecord]{expression: Count[cursorRecord]().Eq(1).expression}
	if _, err := cursorQuery().Where(forged).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("model WHERE accepted aggregate", err)
	}
	deep := Count[cursorRecord]().Eq(1)
	for range MaxExpressionDepth + 1 {
		deep = deep.Not()
	}
	if _, err := groupedReport().Having(deep).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("HAVING depth unbounded", err)
	}
	conditions := make([]HavingPredicate[cursorRecord], MaxExpressionNodes)
	for i := range conditions {
		conditions[i] = Count[cursorRecord]().Eq(1)
	}
	if _, err := groupedReport().Where(id.Eq(1)).Having(conditions...).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("WHERE/HAVING node limits are not shared", err)
	}
}

func TestHavingAndOrderingImplyGrouping(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	node := cursorQuery().modelSelect()
	node.selections = []selectItem{{expression: id.ref}}
	for _, derive := range []func(selectNode) selectNode{
		func(n selectNode) selectNode {
			n.orders = []orderNode{{expression: Count[cursorRecord]().node}}
			return n
		},
		func(n selectNode) selectNode { n.having = []expression{Grouped(id.Eq(1)).expression}; return n },
	} {
		var c compiler
		if _, err := c.selectSQL(derive(node)); !errors.Is(err, fault.Invalid) {
			t.Fatal("implicit grouping allowed ungrouped field", err)
		}
	}
}
