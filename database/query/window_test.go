package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestWindowCompilationSharesBindingsAndDistinctOrdering(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	w := WindowFor(base).PartitionBy(id.Group()).OrderBy(id.Desc()).RowsBetween(Preceding(2), CurrentRow()).ExcludeTies()
	previous := LagOr(id.Value(), 1, int64(99), w)
	q := SelectValue(base.Where(id.Ne(7)), previous).Distinct().OrderBy(previous.Asc()).Limit(4)
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT DISTINCT LAG("records"."id", $1, $2) OVER (PARTITION BY "records"."id" ORDER BY "records"."id" DESC ROWS BETWEEN $3 PRECEDING AND CURRENT ROW EXCLUDE TIES) AS "value" FROM "records" WHERE ("records"."id" <> $4) ORDER BY 1 ASC LIMIT $5`
	if s.SQL() != want || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(99), int64(2), int64(7), int64(4)}) {
		t.Fatal(s.SQL(), s.Arguments())
	}
	_ = w.RowsBetween(CurrentRow(), Following(1)).ExcludeNone().OrderBy(id.Asc())
	again, _ := q.Compile()
	if again.SQL() != s.SQL() || !reflect.DeepEqual(again.Arguments(), s.Arguments()) {
		t.Fatal("window derivation mutated source")
	}
	if _, err := SelectValue(base, RowNumber(Window[cursorRecord]{})).Compile(); err != nil {
		t.Fatal("zero window should mean OVER ()", err)
	}
}

func TestWindowAggregatesUseOrdinaryAggregateCompiler(t *testing.T) {
	base := cursorQuery()
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	w := WindowFor(base).OrderBy(id.Asc())
	for name, expression := range map[string]valueExpression{
		"COUNT(*)":                             Count[cursorRecord]().Over(w).node,
		`COUNT("records"."id")`:                id.Count().Over(w).node,
		`SUM(CAST("records"."id" AS numeric))`: id.Sum().Over(w).node,
		`AVG(CAST("records"."id" AS numeric))`: id.Avg().Over(w).node,
		`MIN("records"."id")`:                  id.Min().Over(w).node,
		`MAX("records"."id")`:                  id.Max().Over(w).node,
	} {
		node := base.modelSelect()
		node.selections = []selectItem{{expression: id.Value().node}, {expression: expression}}
		var c compiler
		s, err := c.compileSelect(node)
		if err != nil || !strings.Contains(s, name+` OVER (ORDER BY "records"."id" ASC)`) || strings.Contains(s, "GROUP BY") {
			t.Fatal(name, s, err)
		}
	}
	if s, err := SelectValue(base, Exists[cursorRecord]().Over(w)).Compile(); err != nil || !strings.Contains(s.SQL(), `(COUNT(*) OVER (ORDER BY "records"."id" ASC) > 0)`) {
		t.Fatal("window existence lost SQL parentheses", err)
	}
	// An ordinary aggregate in a window ORDER BY groups the input SELECT.
	count := Count[cursorRecord]()
	if _, err := SelectValue(base, RowNumber(WindowFor(base).OrderBy(count.Desc()))).Compile(); err != nil {
		t.Fatal("window over grouped rows failed", err)
	}
	if _, err := SelectValue(base, RowNumber(WindowFor(base).OrderBy(count.Desc(), id.Asc()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped window order field accepted", err)
	}
	if _, err := SelectValue(base, Lag(count.Value(), 1, WindowFor(base))).Compile(); err != nil {
		t.Fatal("ordinary aggregate as window input rejected", err)
	}
}

func TestWindowValidationRejectsMalformedAndNestedCalls(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	w := WindowFor(base).OrderBy(id.Asc())
	var missing *Query[cursorRecord]
	for name, window := range map[string]Window[cursorRecord]{
		"nil source":                WindowFor[cursorRecord](missing),
		"zero partition":            w.PartitionBy(Group[cursorRecord]{}),
		"duplicate partition":       w.PartitionBy(id.Group(), id.Group()),
		"nil order":                 w.OrderBy(nil),
		"wrong alias":               w.PartitionBy(NewScalarField[cursorRecord, int64]("other", "id", codec.Signed[int64]()).Group()),
		"negative offset":           w.RowsBetween(Preceding(-1), CurrentRow()),
		"zero boundary":             w.RowsBetween(FramePosition{}, CurrentRow()),
		"nil boundary":              w.RowsBetween(nil, CurrentRow()),
		"reversed kinds":            w.RowsBetween(CurrentRow(), Preceding(1)),
		"unbounded following start": w.RowsBetween(UnboundedFollowing(), UnboundedFollowing()),
		"unbounded preceding end":   w.RowsBetween(UnboundedPreceding(), UnboundedPreceding()),
		"groups without order":      WindowFor(base).GroupsBetween(Preceding(1), CurrentRow()),
		"nested window order":       w.OrderBy(RowNumber(w).Asc()),
		"oversized ordering":        w.OrderBy(make([]ProjectionOrder[cursorRecord], MaxExpressionNodes+1)...),
	} {
		if s, err := SelectValue(base, RowNumber(window)).Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, err)
		}
	}
	for name, expression := range map[string]valueExpression{
		"zero kind":          windowNode{},
		"zero buckets":       NTile(0, w).node,
		"zero nth":           NthValue(id.Value(), 0, w).node,
		"nested input":       Lag(RowNumber(w), 1, w).node,
		"distinct aggregate": id.CountDistinct().Over(w).node,
		"zero value":         FirstValue(Expression[cursorRecord, int64]{}, w).node,
		"nested null":        Lag(Nullable(id.Value()), 1, w).node,
	} {
		node := base.modelSelect()
		node.selections = []selectItem{{expression: expression}}
		var c compiler
		if _, err := c.compileSelect(node); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	// HAVING is not a window evaluation stage even for privately forged nodes.
	node := base.modelSelect()
	node.selections = []selectItem{{expression: Count[cursorRecord]().Value().node}}
	node.having = []expression{typedComparison(RowNumber(w).node, equal, codec.Signed[int64](), []int64{1})}
	var c compiler
	if _, err := c.compileSelect(node); !errors.Is(err, fault.Invalid) {
		t.Fatal("window in HAVING accepted", err)
	}
	if _, err := SelectValue(base, FirstValue(id.Value(), w.RowsBetween(Preceding(7), Preceding(8)))).Compile(); err != nil {
		t.Fatal("legal empty frame rejected", err)
	}
}

func TestWindowSubqueriesAndCorrelationKeepSelectLevels(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	inner := SelectValue(base.Where(id.Ne(5)), RowNumber(WindowFor(base).OrderBy(id.Asc()))).Limit(1)
	outer := SelectValue(base, LagNullable(ScalarQuery(base, inner), 1, WindowFor(base)))
	if s, err := outer.Compile(); err != nil || strings.Count(s.SQL(), " OVER (") != 2 {
		t.Fatal("independent inner window was treated as nested", err)
	}
	alias := As[firstAlias](CTE("numbered", base.Where(id.Ne(9))), "a")
	aid := scopedID(alias.Scope())
	scalar := ScalarQuery(base, SelectValue(alias, aid.Value()).Limit(1))
	if s, err := SelectValue(base, RowNumber(WindowFor(base).OrderBy(scalar.Asc()))).Compile(); err != nil || !strings.HasPrefix(s.SQL(), "WITH ") {
		t.Fatal("window ordering lost CTE dependencies", err)
	}
	link := Correlate(base, As[firstAlias](base, "child"))
	parent := scopedID(OuterScope(link, base.Scope()))
	child := scopedID(InnerScope(link, As[firstAlias](base, "child").Scope()))
	correlated := SelectCorrelatedValue(link, parent.Count().Over(WindowFor(link).PartitionBy(parent.Group()).OrderBy(child.Asc()))).Where(child.EqColumn(parent)).Limit(1)
	sub := correlated.correlatedValue().value.subquery
	changed := requalifyCorrelation(sub, "qualified")
	n := changed.node.selections[0].expression.(windowNode)
	if n.aggregate.field.table != "qualified" || n.window.partitions[0].(fieldRef).table != "qualified" || sub.node.selections[0].expression.(windowNode).window.partitions[0].(fieldRef).table != "records" {
		t.Fatal("window correlation missed qualifier or mutated source")
	}
	if _, err := SelectValue(base, CorrelatedScalarQuery(correlated)).Compile(); err != nil {
		t.Fatal("window aggregate of outer value changed SELECT ownership", err)
	}
}

func TestWindowDepthAndParameterBudgets(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	w := WindowFor(base)
	q := SelectValue(base, LagOr(id.Value(), 1, int64(2), w))
	if _, err := q.Where(wideScoped(base.Scope(), MaxParameters)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("window arguments bypassed parameter budget", err)
	}
	var n windowNode
	n.kind = rowNumberWindow
	n.window.orders = make([]orderNode, 1)
	n.window.orders[0].expression = n
	node := base.modelSelect()
	node.selections = []selectItem{{expression: n}}
	var c compiler
	if _, err := c.compileSelect(node); !errors.Is(err, fault.Invalid) {
		t.Fatal("window cycle bypassed analysis bound", err)
	}
	// Nullable helpers retain one result wrapper.
	nullable := NewNullableField[cursorRecord, int64]("records", "rank", codec.Signed[int64]()).Value()
	var _ Expression[cursorRecord, value.Nullable[int64]] = LagNullable(nullable, 1, w)
	if _, err := SelectValue(base, LeadOr(nullable, 1, value.Of[int64](4), w)).Compile(); err != nil {
		t.Fatal(err)
	}
}
