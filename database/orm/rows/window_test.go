package rows

import (
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestWindowsBindingsNamesAndImmutability(t *testing.T) {
	d := declareAggregateRecords()
	source := FromModel(d.Query())
	v := expr.Value(d.Exact)
	w := expr.WindowFor(ModelScope(d.Query())).PartitionBy(expr.Group(v)).OrderBy(v.Desc()).RowsBetween(expr.Preceding(2), expr.CurrentRow()).ExcludeTies()
	previous := expr.LagOr(expr.Row(v).Value(), 1, int64(99), w)
	q := OrderBy(Distinct(expressionQuery(source, previous, codec.Signed[int64]()).Where(v.Ne(7))), previous.Asc())
	q = Limit(q, 4)
	s := expressionSQL(t, q)
	want := `SELECT DISTINCT LAG("aggregate_records"."exact", $1, $2) OVER (PARTITION BY "aggregate_records"."exact" ORDER BY "aggregate_records"."exact" DESC ROWS BETWEEN $3 PRECEDING AND CURRENT ROW EXCLUDE TIES) AS "value" FROM "aggregate_records" WHERE ("aggregate_records"."exact" <> $4) ORDER BY 1 ASC LIMIT $5`
	if s.SQLText != want || !reflect.DeepEqual(s.Bindings, []any{int64(1), int64(99), int64(2), int64(7), int64(4)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	_ = w.PartitionBy(expr.Group(expr.Value(d.Name))).RowsBetween(expr.CurrentRow(), expr.Following(1)).ExcludeNone().OrderBy(v.Asc())
	again := expressionSQL(t, q)
	if !reflect.DeepEqual(s, again) {
		t.Fatal("window derivation mutated its source")
	}
	named := expr.WindowFor(ModelScope(d.Query())).OrderBy(v.Asc()).Named("ranked")
	same := expr.AddValue(expr.RowNumber(named), expr.Rank(named))
	ns := expressionSQL(t, expressionQuery(source, same, codec.Signed[int64]()))
	if strings.Count(ns.SQLText, `"ranked" AS (`) != 1 || strings.Count(ns.SQLText, `OVER "ranked"`) != 2 {
		t.Fatal(ns.SQLText)
	}
	inherited := named.RowsBetween(expr.UnboundedPreceding(), expr.CurrentRow())
	expressionSQL(t, expressionQuery(source, expr.Over(expr.Count[aggregateRecord](), inherited), codec.Signed[int64]()))
	expressionSQL(t, expressionQuery(source, expr.RowNumber(expr.Window[aggregateRecord]{}), codec.Signed[int64]()))
}

func TestWindowsRejectInvalidDescriptorsBeforeExecution(t *testing.T) {
	d := declareAggregateRecords()
	other := declareAggregateRecords()
	source := FromModel(d.Query())
	v := expr.Value(d.Exact)
	base := expr.WindowFor(ModelScope(d.Query()))
	w := base.OrderBy(v.Asc())
	r := expr.NumericRange(base, expr.Row(v).Value())
	fr := expr.NumericRange(base, expr.Row(expr.Value(d.Approx)).Value())
	invalid := map[string]expr.Window[aggregateRecord]{
		"missing scope":              expr.WindowFor(expr.Scope[aggregateRecord, aggregateRecord]{}),
		"foreign declaration":        expr.WindowFor(ModelScope(other.Query())),
		"foreign partition":          base.PartitionBy(expr.Group(expr.Value(other.Exact))),
		"foreign selected partition": base.PartitionByValues(expr.Sum(other.Exact).Value().Key()),
		"zero partition":             w.PartitionBy(expr.GroupKey[aggregateRecord]{}),
		"duplicate partition":        w.PartitionBy(expr.Group(v), expr.Group(v)),
		"nil order":                  w.OrderBy(nil),
		"nil boundary":               w.RowsBetween(nil, expr.CurrentRow()),
		"zero boundary":              w.RowsBetween(expr.FramePosition{}, expr.CurrentRow()),
		"negative offset":            w.RowsBetween(expr.Preceding(-1), expr.CurrentRow()),
		"backwards boundary":         w.RowsBetween(expr.CurrentRow(), expr.Preceding(1)),
		"invalid end":                w.RowsBetween(expr.UnboundedPreceding(), expr.UnboundedPreceding()),
		"oversized order":            w.OrderBy(make([]expr.ProjectionOrder[aggregateRecord], querycore.MaxExpressionNodes+1)...),
		"invalid start":              w.RowsBetween(expr.UnboundedFollowing(), expr.CurrentRow()),
		"groups need order":          base.GroupsBetween(expr.CurrentRow(), expr.CurrentRow()),
		"nested order":               w.OrderBy(expr.RowNumber(base).Asc()),
		"nested partition":           base.PartitionByValues(expr.RowNumber(base).Key()),
		"bad name":                   w.Named("bad name"),
		"framed inheritance":         w.RowsBetween(expr.CurrentRow(), expr.CurrentRow()).Named("framed").OrderBy(v.Desc()),
		"replace named order":        w.Named("ordered").OrderBy(v.Desc()),
		"add named partition":        w.Named("ordered").PartitionBy(expr.Group(v)),
		"negative range":             r.Between(r.Preceding(-1), r.CurrentRow()),
		"nonfinite range":            fr.Between(fr.Preceding(math.Inf(1)), fr.CurrentRow()),
		"extra range order":          r.Between(r.CurrentRow(), r.Following(2)).OrderBy(v.Asc()),
		"preexisting range order":    expr.NumericRange(w, expr.Row(v).Value()).Between(r.CurrentRow(), r.CurrentRow()),
		"zero range":                 (expr.RangeWindow[aggregateRecord, int64]{}).Between(r.CurrentRow(), r.CurrentRow()),
	}
	for name, window := range invalid {
		t.Run(name, func(t *testing.T) {
			q := expressionQuery(source, expr.RowNumber(window), codec.Signed[int64]())
			state := &driverState{}
			_, err := q.All(t.Context(), openTest(t, state))
			if !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal("invalid window executed", err, state.sql)
			}
		})
	}
	for name, e := range map[string]expr.Selected[aggregateRecord, int64]{
		"distinct aggregate": expr.Over(expr.CountDistinctField(d.Exact), w),
		"nil aggregate":      expr.Over[aggregateRecord, int64](nil, w),
		"nested value":       expr.LagOr(expr.RowNumber(w), 1, int64(0), w),
		"named collision":    expr.AddValue(expr.RowNumber(w.Named("same")), expr.RowNumber(w.Named("same"))),
	} {
		t.Run(name, func(t *testing.T) {
			q := expressionQuery(source, e, codec.Signed[int64]())
			state := &driverState{}
			_, err := q.All(t.Context(), openTest(t, state))
			if !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal(err, state.sql)
			}
		})
	}
	// Grouped keys are accepted, but an ungrouped row field mixed into aggregate ordering is invalid.
	grouped := base.PartitionByValues(expr.Count[aggregateRecord]().Value().Key()).OrderBy(expr.Count[aggregateRecord]().Desc())
	expressionSQL(t, expressionQuery(source, expr.RowNumber(grouped), codec.Signed[int64]()))
	bad := expressionQuery(source, expr.RowNumber(grouped.OrderBy(v.Asc())), codec.Signed[int64]())
	if _, err := querycore.CompileRead(bad.source.Node, t.Context(), false, bad.source.Err, true); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestWindowsResultFamiliesAndHydration(t *testing.T) {
	d := declareAggregateRecords()
	source := FromModel(d.Query())
	w := expr.WindowFor(ModelScope(d.Query())).OrderBy(expr.Value(d.Exact).Asc())
	v := expr.Row(expr.Value(d.Exact)).Value()
	n := expr.Row(expr.Value(d.MaybeExact)).Value()
	for name, e := range map[string]expr.Selected[aggregateRecord, value.Nullable[int64]]{
		"lag": expr.Lag(v, 1, w), "lead": expr.Lead(v, -1, w), "first": expr.FirstValue(v, w), "last": expr.LastValue(v, w), "nth": expr.NthValue(v, 2, w),
		"lag nullable": expr.LagNullable(n, 1, w), "lead nullable": expr.LeadNullable(n, 1, w), "first nullable": expr.FirstNullableValue(n, w), "last nullable": expr.LastNullableValue(n, w), "nth nullable": expr.NthNullableValue(n, 2, w),
		"min": expr.Over(expr.Min(d.Exact), w), "max": expr.Over(expr.Max(d.Exact), w),
	} {
		t.Run(name, func(t *testing.T) {
			q := expressionQuery(source, e, codec.Nullable(codec.Signed[int64]()))
			state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {int64(3)}}}
			got, err := q.All(t.Context(), openTest(t, state))
			if err != nil || len(got) != 2 || !got[0].Value.IsNull() {
				t.Fatal(got, err)
			}
			if n, ok := got[1].Value.Get(); !ok || n != 3 {
				t.Fatal(got)
			}
		})
	}
	for _, e := range []expr.Selected[aggregateRecord, int64]{expr.RowNumber(w), expr.Rank(w), expr.DenseRank(w), expr.LeadOr(v, 1, int64(0), w), expr.Over(expr.CountField(d.Exact).Filter(expr.Value(d.Exact).Ne(0)), w)} {
		expressionSQL(t, expressionQuery(source, e, codec.Signed[int64]()))
	}
	for _, e := range []expr.Selected[aggregateRecord, float64]{expr.PercentRank(w), expr.CumeDist(w)} {
		expressionSQL(t, expressionQuery(source, e, codec.Float[float64]()))
	}
	expressionSQL(t, expressionQuery(source, expr.NTile(3, w), codec.Signed[int32]()))
	expressionSQL(t, expressionQuery(source, expr.Over(expr.Exists[aggregateRecord](), w), codec.Bool[bool]()))
	for _, e := range []expr.Selected[aggregateRecord, int32]{expr.NTile(0, w), expr.NTile(-1, w)} {
		q := expressionQuery(source, e, codec.Signed[int32]())
		if _, err := querycore.CompileRead(q.source.Node, t.Context(), false, q.source.Err, true); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	invalid := expressionQuery(source, expr.Lag(n, 1, w), codec.Nullable(codec.Nullable(codec.Signed[int64]())))
	if _, err := querycore.CompileRead(invalid.source.Node, t.Context(), false, invalid.source.Err, true); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested nullable accepted", err)
	}
}
