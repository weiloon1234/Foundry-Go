package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type aggregateName string
type aggregateRecord struct {
	ID          int64
	Exact       int64
	MaybeExact  value.Nullable[int64]
	Approx      float64
	MaybeApprox value.Nullable[float64]
	Period      temporal.Interval
	MaybePeriod value.Nullable[temporal.Interval]
	Name        aggregateName
	MaybeName   value.Nullable[aggregateName]
}
type aggregateTable struct {
	orm.Table[aggregateRecord, int64]
	Exact       orm.Number[aggregateRecord, int64]
	MaybeExact  orm.NullableNumber[aggregateRecord, int64]
	Approx      orm.Number[aggregateRecord, float64]
	MaybeApprox orm.NullableNumber[aggregateRecord, float64]
	Period      orm.Interval[aggregateRecord]
	MaybePeriod orm.NullableInterval[aggregateRecord]
	Name        orm.Text[aggregateRecord, aggregateName]
	MaybeName   orm.NullableText[aggregateRecord, aggregateName]
}

func declareAggregateRecords() aggregateTable {
	return orm.Define("aggregate_records", func(b orm.Builder[aggregateRecord], r *aggregateRecord) aggregateTable {
		return aggregateTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Exact), orm.NullableNumberOf(b, &r.MaybeExact), orm.NumberOf(b, &r.Approx), orm.NullableNumberOf(b, &r.MaybeApprox), orm.IntervalOf(b, &r.Period), orm.NullableIntervalOf(b, &r.MaybePeriod), orm.TextOf(b, &r.Name), orm.NullableTextOf(b, &r.MaybeName)}
	})
}
func TestAggregateResultFamiliesAndReusableCodecs(t *testing.T) {
	d := declareAggregateRecords()
	source := rows.FromModel(d.Query())
	for name, selected := range map[string]expr.Selected[aggregateRecord, value.Nullable[decimal.Decimal]]{
		"sum": expr.AsSelected(expr.Sum(orm.NumberExpr(d.Exact))), "avg": expr.AsSelected(expr.Avg(orm.NumberExpr(d.Exact))), "nullable_sum": expr.AsSelected(expr.Sum(orm.NullableNumberExpr(d.MaybeExact))), "nullable_avg": expr.AsSelected(expr.Avg(orm.NullableNumberExpr(d.MaybeExact))),
	} {
		t.Run(name, func(t *testing.T) {
			q := expressionQuery(source, selected, codec.Nullable(codec.Decimal()))
			s := expressionSQL(t, q)
			if !strings.Contains(s.SQLText, "CAST(\"aggregate_records\".") || !strings.Contains(s.SQLText, " AS numeric)") {
				t.Fatal(s.SQLText)
			}
			state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"9223372036854775808"}}}
			got, err := rows.Read(q).All(t.Context(), openTest(t, state))
			if err != nil || len(got) != 2 || !got[0].Value.IsNull() {
				t.Fatal(got, err)
			}
			if x, ok := got[1].Value.Get(); !ok || x.String() != "9223372036854775808" {
				t.Fatal(got)
			}
		})
	}
	for _, selected := range []expr.Selected[aggregateRecord, value.Nullable[float64]]{expr.AsSelected(expr.SumFloat(orm.NumberExpr(d.Approx))), expr.AsSelected(expr.AvgFloat(orm.NumberExpr(d.Approx))), expr.AsSelected(expr.SumFloat(orm.NullableNumberExpr(d.MaybeApprox))), expr.AsSelected(expr.AvgFloat(orm.NullableNumberExpr(d.MaybeApprox)))} {
		s := expressionSQL(t, expressionQuery(source, selected, codec.Nullable(codec.Float[float64]())))
		if !strings.Contains(s.SQLText, " AS double precision)") {
			t.Fatal(s.SQLText)
		}
	}
	for _, selected := range []expr.Selected[aggregateRecord, value.Nullable[temporal.Interval]]{expr.AsSelected(expr.SumInterval(orm.IntervalExpr(d.Period))), expr.AsSelected(expr.AvgInterval(orm.IntervalExpr(d.Period))), expr.AsSelected(expr.SumInterval(orm.NullableIntervalExpr(d.MaybePeriod))), expr.AsSelected(expr.AvgInterval(orm.NullableIntervalExpr(d.MaybePeriod)))} {
		s := expressionSQL(t, expressionQuery(source, selected, codec.Nullable(codec.Interval())))
		if !strings.Contains(s.SQLText, " AS interval)") {
			t.Fatal(s.SQLText)
		}
	}
	// Reusable aggregate descriptors give relation loaders an actual result scanner,
	// preserving named scalar types and NULL without rebuilding a parallel codec.
	for _, aggregate := range []expr.AggregateExpression[aggregateRecord, value.Nullable[aggregateName]]{expr.Min(orm.TextExpr(d.Name)), expr.Max(orm.NullableTextExpr(d.MaybeName))} {
		core, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if core.Err != nil || column.Scan == nil {
			t.Fatal(core.Err, column)
		}
		var decoded value.Nullable[aggregateName]
		scanner := column.Scan(&decoded).(sql.Scanner)
		if err := scanner.Scan("Ada"); err != nil {
			t.Fatal(err)
		}
		if x, ok := decoded.Get(); !ok || x != aggregateName("Ada") {
			t.Fatal(decoded)
		}
		if err := scanner.Scan(nil); err != nil || !decoded.IsNull() {
			t.Fatal(decoded, err)
		}
	}
	for _, selected := range []expr.Selected[aggregateRecord, value.Nullable[aggregateName]]{expr.AsSelected(expr.Min(orm.TextExpr(d.Name))), expr.AsSelected(expr.Max(orm.TextExpr(d.Name))), expr.AsSelected(expr.Min(orm.NullableTextExpr(d.MaybeName))), expr.AsSelected(expr.Max(orm.NullableTextExpr(d.MaybeName)))} {
		expressionSQL(t, expressionQuery(source, selected, codec.Nullable(codec.String[aggregateName]())))
	}
	for _, a := range []expr.AggregateExpression[aggregateRecord, int64]{expr.Count[aggregateRecord](), expr.CountField(orm.NullableNumberExpr(d.MaybeExact)), expr.CountDistinctField(orm.TextExpr(d.Name))} {
		core, column := expr.FoundryAggregate(a, sqlowner.Expressions())
		if core.Err != nil || column.Scan == nil {
			t.Fatal(core, column)
		}
	}
}

// The aggregate's Param uses its present comparison type, so the fallback
// removes nullability without a caller-supplied codec or nullable wrapper.
func TestNullableAggregateParamRequiredFallback(t *testing.T) {
	d := declareAggregateRecords()
	source := rows.FromModel(d.Query())
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, decimal.Decimal]{expr.Sum(orm.NumberExpr(d.Exact)), expr.Avg(orm.NullableNumberExpr(d.MaybeExact))} {
		var fallback expr.RowExpression[aggregateRecord, decimal.Decimal] = expr.ParamOf(aggregate, decimal.FromInt64(0))
		checkAggregateFallback(t, source, expr.AsSelected(aggregate), fallback, codec.Decimal(), "0", decimal.FromInt64(0))
	}
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, float64]{expr.SumFloat(orm.NumberExpr(d.Approx)), expr.AvgFloat(orm.NullableNumberExpr(d.MaybeApprox))} {
		checkAggregateFallback(t, source, expr.AsSelected(aggregate), expr.ParamOf(aggregate, 0), codec.Float[float64](), float64(0), float64(0))
	}
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, temporal.Interval]{expr.SumInterval(orm.IntervalExpr(d.Period)), expr.AvgInterval(orm.NullableIntervalExpr(d.MaybePeriod))} {
		zero := temporal.Interval{}
		checkAggregateFallback(t, source, expr.AsSelected(aggregate), expr.ParamOf(aggregate, zero), codec.Interval(), zero.String(), zero)
	}
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, aggregateName]{expr.Min(orm.TextExpr(d.Name)), expr.Max(orm.NullableTextExpr(d.MaybeName))} {
		checkAggregateFallback(t, source, expr.AsSelected(aggregate), expr.ParamOf(aggregate, aggregateName("")), codec.String[aggregateName](), "", aggregateName(""))
	}
	alias := rows.As[left](d.Query(), "a")
	total := expr.Filter(expr.SumAt(rows.Scope(alias), orm.NullableNumberExpr(d.MaybeExact)), expr.Eq(expr.At(rows.Scope(alias), orm.NumberExpr(d.Exact)), 1))
	fallback := expr.ParamOf(total, decimal.FromInt64(0))
	// Param captures a standalone value; it must not evaluate the aggregate or its filter.
	expressionSQL(t, expressionQuery(alias, expr.AsSelected(fallback), codec.Decimal()))
	checkAggregateFallback(t, alias, expr.AsSelected(total), fallback, codec.Decimal(), "0", decimal.FromInt64(0))
}

func checkAggregateFallback[S, V any](t *testing.T, source rows.SelectionSource[S], aggregate expr.Selected[S, value.Nullable[V]], fallback expr.RowExpression[S, V], c codec.Codec[V], bound driver.Value, want V) {
	t.Helper()
	q := expressionQuery(source, expr.CoalesceValue(aggregate, expr.AsSelected(fallback)), c)
	statement := expressionSQL(t, q)
	if !strings.Contains(statement.SQLText, "COALESCE(") || len(statement.Bindings) == 0 || !reflect.DeepEqual(statement.Bindings[len(statement.Bindings)-1], bound) {
		t.Fatal(statement)
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{bound}}}
	got, err := rows.Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Value, want) {
		t.Fatal(got, err)
	}
}

func TestGroupedFilteredAggregatesAndSelectedOrdering(t *testing.T) {
	o := declareOrders()
	source := rows.As[right](o.Query(), "o")
	scope := rows.Scope(source)
	amount := expr.At(scope, orm.NumberExpr(o.MemberID))
	base := expr.Filter(expr.SumAt(scope, orm.NumberExpr(o.MemberID)), expr.Gt(expr.OrderRow(amount), 0))

	derived := expr.Filter(base, expr.Ne(amount, 2))
	originalCore, _ := expr.FoundryAggregate(base, sqlowner.Expressions())
	derivedCore, _ := expr.FoundryAggregate(derived, sqlowner.Expressions())
	if len(originalCore.Node.(querycore.AggregateNode).Filter.Predicates) != 1 || len(derivedCore.Node.(querycore.AggregateNode).Filter.Predicates) != 2 {
		t.Fatal("aggregate filter mutated its ancestor")
	}
	type report struct {
		Group int64
		Count int64
		Total value.Nullable[decimal.Decimal]
	}
	type fields struct {
		rows.Row[report]
		Group, Count rows.Field[report, int64]
		Total        rows.Field[report, value.Nullable[decimal.Decimal]]
	}
	schema := rows.Define(func(b rows.Builder[report], r *report) fields {
		return fields{b.Row(), rows.NumberOf(b, &r.Group), rows.NumberOf(b, &r.Count), rows.NullableNumberOf(b, &r.Total)}
	})
	count := expr.Filter(expr.Count[rows.Alias[right, order]](), expr.Ne(amount, 0))
	q := rows.Select(source, schema, rows.Map(schema.Group, amount), rows.MapSelected(schema.Count, expr.AsSelected(count)), rows.MapSelected(schema.Total, expr.AsSelected(derived)))
	q = rows.GroupBy(q, expr.Group(amount))
	q = rows.Having(q, expr.HavingAnd(expr.GtValue(count, 0), expr.HavingOr(expr.IsNullValue(base), expr.HavingNot(expr.GtValue(base, decimal.FromInt64(1))))))
	q = rows.OrderBy(q, expr.DescValue(count), expr.AscValue(derived).NullsLast(), expr.Asc(amount))
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, "FILTER (WHERE") || !strings.Contains(s.SQLText, " GROUP BY ") || !strings.Contains(s.SQLText, " HAVING ") || !strings.Contains(s.SQLText, "NULLS LAST") {
		t.Fatal(s.SQLText)
	}
	state := &driverState{columns: []string{"group", "count", "total"}, rows: [][]driver.Value{{int64(1), int64(2), "5"}}}
	got, err := rows.Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Count != 2 {
		t.Fatal(got, err)
	}
}

func TestAggregateScopedFamiliesAndOwnership(t *testing.T) {
	d, other := declareAggregateRecords(), declareAggregateRecords()
	a := rows.As[left](d.Query(), "a")
	scope := rows.Scope(a)
	for _, selected := range []expr.Selected[rows.Alias[left, aggregateRecord], value.Nullable[decimal.Decimal]]{expr.AsSelected(expr.SumAt(scope, orm.NumberExpr(d.Exact))), expr.AsSelected(expr.AvgAt(scope, orm.NullableNumberExpr(d.MaybeExact)))} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.Decimal())))
	}
	for _, selected := range []expr.Selected[rows.Alias[left, aggregateRecord], value.Nullable[float64]]{expr.AsSelected(expr.SumFloatAt(scope, orm.NumberExpr(d.Approx))), expr.AsSelected(expr.AvgFloatAt(scope, orm.NullableNumberExpr(d.MaybeApprox)))} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.Float[float64]())))
	}
	for _, selected := range []expr.Selected[rows.Alias[left, aggregateRecord], value.Nullable[temporal.Interval]]{expr.AsSelected(expr.SumIntervalAt(scope, orm.IntervalExpr(d.Period))), expr.AsSelected(expr.AvgIntervalAt(scope, orm.NullableIntervalExpr(d.MaybePeriod)))} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.Interval())))
	}
	for _, selected := range []expr.Selected[rows.Alias[left, aggregateRecord], value.Nullable[aggregateName]]{expr.AsSelected(expr.MinAt(scope, orm.TextExpr(d.Name))), expr.AsSelected(expr.MaxAt(scope, orm.NullableTextExpr(d.MaybeName)))} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.String[aggregateName]())))
	}
	for _, selected := range []expr.Selected[rows.Alias[left, aggregateRecord], int64]{expr.AsSelected(expr.CountFieldAt(scope, orm.NullableTextExpr(d.MaybeName))), expr.AsSelected(expr.CountDistinctFieldAt(scope, orm.TextExpr(d.Name)))} {
		expressionSQL(t, expressionQuery(a, selected, codec.Signed[int64]()))
	}
	own := expr.At(scope, orm.NumberExpr(d.Exact))
	foreign := expr.At(scope, orm.NumberExpr(other.Exact))
	for _, selected := range []expr.Selected[rows.Alias[left, aggregateRecord], value.Nullable[decimal.Decimal]]{
		expr.AsSelected(expr.SumAt(scope, orm.NumberExpr(other.Exact))),
		expr.AsSelected(expr.Filter(expr.SumAt(scope, orm.NumberExpr(d.Exact)), expr.Eq(foreign, 1))),
		expr.AsSelected(expr.Filter(expr.SumAt(scope, orm.NumberExpr(d.Exact)), expr.Predicate[rows.Alias[left, aggregateRecord]]{})),
		expr.Nullable(expr.AsSelected(expr.ParamOf(expr.SumAt(scope, orm.NumberExpr(other.Exact)), decimal.FromInt64(1)))),
	} {
		state := &driverState{}
		got, err := rows.Read(expressionQuery(a, selected, codec.Nullable(codec.Decimal()))).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("aggregate lost ownership/error", got, err, state.sql)
		}
	}
	// Every clause retains scope/declaration checks, including reference-free keys.
	q := expressionQuery(a, expr.AsSelected(expr.Row(own)), codec.Signed[int64]())
	for _, invalid := range []rows.Query[rows.Alias[left, aggregateRecord], expressionRow[int64]]{
		rows.GroupBy(q, expr.Group(foreign)), rows.Having(q, expr.Grouped(expr.Eq(foreign, 1))),
		rows.OrderBy(q, expr.AscValue(expr.AsSelected(expr.Row(foreign)))), rows.DistinctOn(q, expr.Group(foreign)), rows.DistinctOnValues(q, expr.Key(expr.AsSelected(expr.Row(foreign)))),
		rows.GroupBy(q, expr.GroupKey[rows.Alias[left, aggregateRecord]]{}), rows.Having(q, expr.HavingPredicate[rows.Alias[left, aggregateRecord]]{}),
		rows.DistinctOnValues(q, expr.ProjectionKey[rows.Alias[left, aggregateRecord]]{}), rows.OrderBy(q, expr.SelectedOrder[rows.Alias[left, aggregateRecord]]{}),
	} {
		state := &driverState{}
		got, err := rows.Read(invalid).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("clause lost ownership/error", got, err, state.sql)
		}
	}
}

func TestDistinctReadOnlyModelsAndGroupingRules(t *testing.T) {
	o := declareOrders()
	original := rows.FromModel(o.Query())
	key := expr.Value(orm.NumberExpr(o.MemberID))
	on := rows.OrderBy(rows.DistinctOn(original, expr.Group(key)), expr.Asc(key), expr.Desc(expr.Value(orm.KeyExpr(o.ID))))
	plain := rows.Distinct(on)
	if rows.SourceOf(original).Node.Distinct.Kind != querycore.NoDistinct || rows.SourceOf(on).Node.Distinct.Kind != querycore.DistinctOn || rows.SourceOf(plain).Node.Distinct.Kind != querycore.DistinctRows {
		t.Fatal("DISTINCT derivation mutated ancestors")
	}
	if len(rows.SourceOf(plain).Node.Distinct.Keys) != 0 {
		t.Fatal("DISTINCT retained ON keys")
	}
	state := &driverState{columns: []string{"id", "member_id"}, rows: [][]driver.Value{{int64(2), int64(1)}}}
	got, err := rows.Read(on).All(t.Context(), openTest(t, state))
	if err != nil || !reflect.DeepEqual(got, []order{{ID: 2, MemberID: 1}}) || !strings.Contains(state.sql, "SELECT DISTINCT ON") {
		t.Fatal(got, err, state.sql)
	}
	// DISTINCT computed ordering reuses selected parameter identities.
	x := expr.Add(expr.Value(orm.NumberExpr(o.MemberID)), expr.Param(orm.NumberExpr(o.MemberID), 1))
	q := rows.OrderBy(rows.Distinct(expressionQuery(original, expr.AsSelected(x), codec.Signed[int64]())), expr.DescValue(expr.AsSelected(x)))
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, "ORDER BY 1 DESC") || !reflect.DeepEqual(s.Bindings, []any{int64(1)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	count := expr.Count[order]()
	selected := expressionQuery(original, expr.AsSelected(count), codec.Signed[int64]())
	expressionSQL(t, rows.OrderBy(rows.DistinctOnValues(selected, expr.Key(expr.AsSelected(count))), expr.DescValue(count)))
	// Shared PostgreSQL validation rejects incomplete grouping and ON/order conflicts.
	for _, invalid := range []rows.Query[order, expressionRow[int64]]{
		rows.GroupBy(expressionQuery(original, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(o.ID)))), codec.Signed[int64]()), expr.Group(key)),
		rows.Having(expressionQuery(original, expr.AsSelected(expr.Row(key)), codec.Signed[int64]()), expr.GtValue(count, 1)),
		rows.OrderBy(rows.DistinctOn(expressionQuery(original, expr.AsSelected(expr.Row(key)), codec.Signed[int64]()), expr.Group(key)), expr.Asc(expr.Value(orm.KeyExpr(o.ID)))),
		rows.DistinctOn(selected),
	} {
		state := &driverState{}
		out, err := rows.Read(invalid).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || out != nil || state.sql != "" {
			t.Fatal("invalid grouped/distinct query accepted", out, err, state.sql)
		}
	}
}

func TestAggregateResultRepresentations(t *testing.T) {
	d := declareAggregateRecords()
	source := rows.From(d.Query())
	avg := rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.Avg(orm.NumberExpr(d.Exact)))))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"1.5000000000000000"}}}
	got, err := avg.RequireFirst(t.Context(), openTest(t, state))
	v, ok := got.Get()
	if err != nil || !ok || v.String() != "1.5" {
		t.Fatal("integer average precision", got, err)
	}
	sum := rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.Sum(orm.NumberExpr(d.Exact)))))
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}}}
	null, err := sum.RequireFirst(t.Context(), openTest(t, state))
	if err != nil || !null.IsNull() {
		t.Fatal(null, err)
	}
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{float64(1.5)}}}
	if _, err := sum.RequireFirst(t.Context(), openTest(t, state)); err == nil {
		t.Fatal("approximate result accepted by exact aggregate")
	}
	type row struct {
		ID     int64
		Amount float32
	}
	type table struct {
		orm.Table[row, int64]
		Amount orm.Number[row, float32]
	}
	f := orm.Define("floats", func(b orm.Builder[row], r *row) table {
		return table{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Amount)}
	})
	floating := rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.AsSelected(expr.AvgFloat(orm.NumberExpr(f.Amount)))))
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{float64(1.5)}}}
	result, err := floating.RequireFirst(t.Context(), openTest(t, state))
	n, present := result.Get()
	if err != nil || !present || n != 1.5 {
		t.Fatal("float32 average must decode float64", result, err)
	}
}

func TestHavingCapturesMembershipAndDerivations(t *testing.T) {
	d := declareAggregateRecords()
	source := rows.From(d.Query())
	count := expr.Count[aggregateRecord]()
	id := expr.Value(orm.NumberExpr(d.Exact))
	values := []int64{1, 2}
	q := rows.GroupByScalar(rows.SelectScalar(source, expr.AsSelected(count)), expr.Group(id))
	q = rows.HavingScalar(q, expr.HavingAnd(expr.GtValue(count, 2), expr.HavingNot(expr.Grouped(expr.In(expr.OrderRow(id), values...)))))
	values[0] = 999
	statement, err := rows.CompileRead(rows.ReadScalar(q))
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{int64(2), "{1,2}"}) {
		t.Fatal(statement, err)
	}
	derived := rows.HavingScalar(q, expr.LtValue(count, 10))
	if _, err := rows.CompileRead(rows.ReadScalar(derived)); err != nil {
		t.Fatal(err)
	}
	again, err := rows.CompileRead(rows.ReadScalar(q))
	if err != nil || again.SQL() != statement.SQL() || !reflect.DeepEqual(again.Arguments(), statement.Arguments()) {
		t.Fatal("HAVING derivation mutated source", again, err)
	}
}
