package rows

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
	source := FromModel(d.Query())
	for name, selected := range map[string]expr.Selected[aggregateRecord, value.Nullable[decimal.Decimal]]{
		"sum": expr.Sum(d.Exact).Value(), "avg": expr.Avg(d.Exact).Value(), "nullable_sum": expr.Sum(d.MaybeExact).Value(), "nullable_avg": expr.Avg(d.MaybeExact).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			q := expressionQuery(source, selected, codec.Nullable(codec.Decimal()))
			s := expressionSQL(t, q)
			if !strings.Contains(s.SQLText, "CAST(\"aggregate_records\".") || !strings.Contains(s.SQLText, " AS numeric)") {
				t.Fatal(s.SQLText)
			}
			state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"9223372036854775808"}}}
			got, err := Read(q).All(t.Context(), openTest(t, state))
			if err != nil || len(got) != 2 || !got[0].Value.IsNull() {
				t.Fatal(got, err)
			}
			if x, ok := got[1].Value.Get(); !ok || x.String() != "9223372036854775808" {
				t.Fatal(got)
			}
		})
	}
	for _, selected := range []expr.Selected[aggregateRecord, value.Nullable[float64]]{expr.SumFloat(d.Approx).Value(), expr.AvgFloat(d.Approx).Value(), expr.SumFloat(d.MaybeApprox).Value(), expr.AvgFloat(d.MaybeApprox).Value()} {
		s := expressionSQL(t, expressionQuery(source, selected, codec.Nullable(codec.Float[float64]())))
		if !strings.Contains(s.SQLText, " AS double precision)") {
			t.Fatal(s.SQLText)
		}
	}
	for _, selected := range []expr.Selected[aggregateRecord, value.Nullable[temporal.Interval]]{expr.SumInterval(d.Period).Value(), expr.AvgInterval(d.Period).Value(), expr.SumInterval(d.MaybePeriod).Value(), expr.AvgInterval(d.MaybePeriod).Value()} {
		s := expressionSQL(t, expressionQuery(source, selected, codec.Nullable(codec.Interval())))
		if !strings.Contains(s.SQLText, " AS interval)") {
			t.Fatal(s.SQLText)
		}
	}
	// Reusable aggregate descriptors give relation loaders an actual result scanner,
	// preserving named scalar types and NULL without rebuilding a parallel codec.
	for _, aggregate := range []expr.AggregateExpression[aggregateRecord, value.Nullable[aggregateName]]{expr.Min(d.Name), expr.Max(d.MaybeName)} {
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
	for _, selected := range []expr.Selected[aggregateRecord, value.Nullable[aggregateName]]{expr.Min(d.Name).Value(), expr.Max(d.Name).Value(), expr.Min(d.MaybeName).Value(), expr.Max(d.MaybeName).Value()} {
		expressionSQL(t, expressionQuery(source, selected, codec.Nullable(codec.String[aggregateName]())))
	}
	for _, a := range []expr.AggregateExpression[aggregateRecord, int64]{expr.Count[aggregateRecord](), expr.CountField(d.MaybeExact), expr.CountDistinctField(d.Name)} {
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
	source := FromModel(d.Query())
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, decimal.Decimal]{expr.Sum(d.Exact), expr.Avg(d.MaybeExact)} {
		var fallback expr.RowExpression[aggregateRecord, decimal.Decimal] = aggregate.Param(decimal.FromInt64(0))
		checkAggregateFallback(t, source, aggregate.Value(), fallback, codec.Decimal(), "0", decimal.FromInt64(0))
	}
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, float64]{expr.SumFloat(d.Approx), expr.AvgFloat(d.MaybeApprox)} {
		checkAggregateFallback(t, source, aggregate.Value(), aggregate.Param(0), codec.Float[float64](), float64(0), float64(0))
	}
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, temporal.Interval]{expr.SumInterval(d.Period), expr.AvgInterval(d.MaybePeriod)} {
		zero := temporal.Interval{}
		checkAggregateFallback(t, source, aggregate.Value(), aggregate.Param(zero), codec.Interval(), zero.String(), zero)
	}
	for _, aggregate := range []expr.NullableOrderedAggregate[aggregateRecord, aggregateName]{expr.Min(d.Name), expr.Max(d.MaybeName)} {
		checkAggregateFallback(t, source, aggregate.Value(), aggregate.Param(aggregateName("")), codec.String[aggregateName](), "", aggregateName(""))
	}
	alias := As[left](d.Query(), "a")
	total := expr.SumAt(Scope(alias), d.MaybeExact).Filter(expr.At(Scope(alias), d.Exact).Eq(1))
	fallback := total.Param(decimal.FromInt64(0))
	// Param captures a standalone value; it must not evaluate the aggregate or its filter.
	expressionSQL(t, expressionQuery(alias, fallback.Value(), codec.Decimal()))
	checkAggregateFallback(t, alias, total.Value(), fallback, codec.Decimal(), "0", decimal.FromInt64(0))
}

func checkAggregateFallback[S, V any](t *testing.T, source SelectionSource[S], aggregate expr.Selected[S, value.Nullable[V]], fallback expr.RowExpression[S, V], c codec.Codec[V], bound driver.Value, want V) {
	t.Helper()
	q := expressionQuery(source, expr.CoalesceValue(aggregate, fallback.Value()), c)
	statement := expressionSQL(t, q)
	if !strings.Contains(statement.SQLText, "COALESCE(") || len(statement.Bindings) == 0 || !reflect.DeepEqual(statement.Bindings[len(statement.Bindings)-1], bound) {
		t.Fatal(statement)
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{bound}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Value, want) {
		t.Fatal(got, err)
	}
}

func TestGroupedFilteredAggregatesAndSelectedOrdering(t *testing.T) {
	o := declareOrders()
	source := As[right](o.Query(), "o")
	scope := Scope(source)
	amount := expr.At(scope, o.MemberID)
	base := expr.SumAt(scope, o.MemberID).Filter(expr.OrderRow(amount).Gt(0))

	derived := base.Filter(amount.Ne(2))
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
		Row[report]
		Group, Count Field[report, int64]
		Total        Field[report, value.Nullable[decimal.Decimal]]
	}
	schema := Define(func(b Builder[report], r *report) fields {
		return fields{b.Row(), NumberOf(b, &r.Group), NumberOf(b, &r.Count), NullableNumberOf(b, &r.Total)}
	})
	count := expr.Count[Alias[right, order]]().Filter(amount.Ne(0))
	q := Select(source, schema, Map(schema.Group, amount), MapSelected(schema.Count, count.Value()), MapSelected(schema.Total, derived.Value()))
	q = GroupBy(q, expr.Group(amount))
	q = Having(q, expr.HavingAnd(count.Gt(0), expr.HavingOr(base.IsNull(), base.Gt(decimal.FromInt64(1)).Not())))
	q = OrderBy(q, count.Desc(), derived.Asc().NullsLast(), amount.Asc())
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, "FILTER (WHERE") || !strings.Contains(s.SQLText, " GROUP BY ") || !strings.Contains(s.SQLText, " HAVING ") || !strings.Contains(s.SQLText, "NULLS LAST") {
		t.Fatal(s.SQLText)
	}
	state := &driverState{columns: []string{"group", "count", "total"}, rows: [][]driver.Value{{int64(1), int64(2), "5"}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Count != 2 {
		t.Fatal(got, err)
	}
}

func TestAggregateScopedFamiliesAndOwnership(t *testing.T) {
	d, other := declareAggregateRecords(), declareAggregateRecords()
	a := As[left](d.Query(), "a")
	scope := Scope(a)
	for _, selected := range []expr.Selected[Alias[left, aggregateRecord], value.Nullable[decimal.Decimal]]{expr.SumAt(scope, d.Exact).Value(), expr.AvgAt(scope, d.MaybeExact).Value()} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.Decimal())))
	}
	for _, selected := range []expr.Selected[Alias[left, aggregateRecord], value.Nullable[float64]]{expr.SumFloatAt(scope, d.Approx).Value(), expr.AvgFloatAt(scope, d.MaybeApprox).Value()} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.Float[float64]())))
	}
	for _, selected := range []expr.Selected[Alias[left, aggregateRecord], value.Nullable[temporal.Interval]]{expr.SumIntervalAt(scope, d.Period).Value(), expr.AvgIntervalAt(scope, d.MaybePeriod).Value()} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.Interval())))
	}
	for _, selected := range []expr.Selected[Alias[left, aggregateRecord], value.Nullable[aggregateName]]{expr.MinAt(scope, d.Name).Value(), expr.MaxAt(scope, d.MaybeName).Value()} {
		expressionSQL(t, expressionQuery(a, selected, codec.Nullable(codec.String[aggregateName]())))
	}
	for _, selected := range []expr.Selected[Alias[left, aggregateRecord], int64]{expr.CountFieldAt(scope, d.MaybeName).Value(), expr.CountDistinctFieldAt(scope, d.Name).Value()} {
		expressionSQL(t, expressionQuery(a, selected, codec.Signed[int64]()))
	}
	own := expr.At(scope, d.Exact)
	foreign := expr.At(scope, other.Exact)
	for _, selected := range []expr.Selected[Alias[left, aggregateRecord], value.Nullable[decimal.Decimal]]{
		expr.SumAt(scope, other.Exact).Value(),
		expr.SumAt(scope, d.Exact).Filter(foreign.Eq(1)).Value(),
		expr.SumAt(scope, d.Exact).Filter(expr.Predicate[Alias[left, aggregateRecord]]{}).Value(),
		expr.Nullable(expr.SumAt(scope, other.Exact).Param(decimal.FromInt64(1)).Value()),
	} {
		state := &driverState{}
		got, err := Read(expressionQuery(a, selected, codec.Nullable(codec.Decimal()))).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("aggregate lost ownership/error", got, err, state.sql)
		}
	}
	// Every clause retains scope/declaration checks, including reference-free keys.
	q := expressionQuery(a, expr.Row(own).Value(), codec.Signed[int64]())
	for _, invalid := range []Query[Alias[left, aggregateRecord], expressionRow[int64]]{
		GroupBy(q, expr.Group(foreign)), Having(q, expr.Grouped(foreign.Eq(1))),
		OrderBy(q, expr.Row(foreign).Value().Asc()), DistinctOn(q, expr.Group(foreign)), DistinctOnValues(q, expr.Row(foreign).Value().Key()),
		GroupBy(q, expr.GroupKey[Alias[left, aggregateRecord]]{}), Having(q, expr.HavingPredicate[Alias[left, aggregateRecord]]{}),
		DistinctOnValues(q, expr.ProjectionKey[Alias[left, aggregateRecord]]{}), OrderBy(q, expr.SelectedOrder[Alias[left, aggregateRecord]]{}),
	} {
		state := &driverState{}
		got, err := Read(invalid).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("clause lost ownership/error", got, err, state.sql)
		}
	}
}

func TestDistinctReadOnlyModelsAndGroupingRules(t *testing.T) {
	o := declareOrders()
	original := FromModel(o.Query())
	key := expr.Value(o.MemberID)
	on := OrderBy(DistinctOn(original, expr.Group(key)), key.Asc(), expr.Value(o.ID).Desc())
	plain := Distinct(on)
	if original.source.Node.Distinct.Kind != querycore.NoDistinct || on.source.Node.Distinct.Kind != querycore.DistinctOn || plain.source.Node.Distinct.Kind != querycore.DistinctRows {
		t.Fatal("DISTINCT derivation mutated ancestors")
	}
	if len(plain.source.Node.Distinct.Keys) != 0 {
		t.Fatal("DISTINCT retained ON keys")
	}
	state := &driverState{columns: []string{"id", "member_id"}, rows: [][]driver.Value{{int64(2), int64(1)}}}
	got, err := Read(on).All(t.Context(), openTest(t, state))
	if err != nil || !reflect.DeepEqual(got, []order{{ID: 2, MemberID: 1}}) || !strings.Contains(state.sql, "SELECT DISTINCT ON") {
		t.Fatal(got, err, state.sql)
	}
	// DISTINCT computed ordering reuses selected parameter identities.
	x := expr.Add(expr.Value(o.MemberID), expr.Param(o.MemberID, 1))
	q := OrderBy(Distinct(expressionQuery(original, x.Value(), codec.Signed[int64]())), x.Value().Desc())
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, "ORDER BY 1 DESC") || !reflect.DeepEqual(s.Bindings, []any{int64(1)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	count := expr.Count[order]()
	selected := expressionQuery(original, count.Value(), codec.Signed[int64]())
	expressionSQL(t, OrderBy(DistinctOnValues(selected, count.Value().Key()), count.Desc()))
	// Shared PostgreSQL validation rejects incomplete grouping and ON/order conflicts.
	for _, invalid := range []Query[order, expressionRow[int64]]{
		GroupBy(expressionQuery(original, expr.Row(expr.Value(o.ID)).Value(), codec.Signed[int64]()), expr.Group(key)),
		Having(expressionQuery(original, expr.Row(key).Value(), codec.Signed[int64]()), count.Gt(1)),
		OrderBy(DistinctOn(expressionQuery(original, expr.Row(key).Value(), codec.Signed[int64]()), expr.Group(key)), expr.Value(o.ID).Asc()),
		DistinctOn(selected),
	} {
		state := &driverState{}
		out, err := Read(invalid).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || out != nil || state.sql != "" {
			t.Fatal("invalid grouped/distinct query accepted", out, err, state.sql)
		}
	}
}

func TestAggregateResultRepresentations(t *testing.T) {
	d := declareAggregateRecords()
	source := From(d.Query())
	avg := ReadScalar(SelectScalar(source, expr.Avg(d.Exact).Value()))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"1.5000000000000000"}}}
	got, err := avg.RequireFirst(t.Context(), openTest(t, state))
	v, ok := got.Get()
	if err != nil || !ok || v.String() != "1.5" {
		t.Fatal("integer average precision", got, err)
	}
	sum := ReadScalar(SelectScalar(source, expr.Sum(d.Exact).Value()))
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
	floating := ReadScalar(SelectScalar(From(f.Query()), expr.AvgFloat(f.Amount).Value()))
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{float64(1.5)}}}
	result, err := floating.RequireFirst(t.Context(), openTest(t, state))
	n, present := result.Get()
	if err != nil || !present || n != 1.5 {
		t.Fatal("float32 average must decode float64", result, err)
	}
}

func TestHavingCapturesMembershipAndDerivations(t *testing.T) {
	d := declareAggregateRecords()
	source := From(d.Query())
	count := expr.Count[aggregateRecord]()
	id := expr.Value(d.Exact)
	values := []int64{1, 2}
	q := GroupByScalar(SelectScalar(source, count.Value()), expr.Group(id))
	q = HavingScalar(q, expr.HavingAnd(count.Gt(2), expr.Grouped(expr.OrderRow(id).In(values...)).Not()))
	values[0] = 999
	statement, err := CompileRead(ReadScalar(q))
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{int64(2), "{1,2}"}) {
		t.Fatal(statement, err)
	}
	derived := HavingScalar(q, count.Lt(10))
	if _, err := CompileRead(ReadScalar(derived)); err != nil {
		t.Fatal(err)
	}
	again, err := CompileRead(ReadScalar(q))
	if err != nil || again.SQL() != statement.SQL() || !reflect.DeepEqual(again.Arguments(), statement.Arguments()) {
		t.Fatal("HAVING derivation mutated source", again, err)
	}
}
