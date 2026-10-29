package pgsmoke_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

type aggregateSide struct{}
type aggregateSample struct {
	ID        int64
	Bucket    int64
	Required  int64
	Exact     value.Nullable[int64]
	Approx    value.Nullable[float64]
	Period    value.Nullable[temporal.Interval]
	Label     value.Nullable[string]
	DeletedAt value.Nullable[temporal.DateTime]
}
type aggregateSamples struct {
	orm.Table[aggregateSample, int64]
	Bucket, Required orm.Number[aggregateSample, int64]
	Exact            orm.NullableNumber[aggregateSample, int64]
	Approx           orm.NullableNumber[aggregateSample, float64]
	Period           orm.NullableInterval[aggregateSample]
	Label            orm.NullableText[aggregateSample, string]
	DeletedAt        orm.SoftDelete[aggregateSample, temporal.DateTime]
}
type aggregateReport struct {
	Bucket        int64
	Count         int64
	Present       int64
	Distinct      int64
	Total         value.Nullable[decimal.Decimal]
	Average       value.Nullable[decimal.Decimal]
	FloatTotal    value.Nullable[float64]
	FloatAverage  value.Nullable[float64]
	PeriodTotal   value.Nullable[temporal.Interval]
	PeriodAverage value.Nullable[temporal.Interval]
	Min           value.Nullable[string]
	Max           value.Nullable[string]
	Filtered      int64
	FilteredTotal value.Nullable[decimal.Decimal]
}
type aggregateFields struct {
	rows.Row[aggregateReport]
	Bucket        rows.Field[aggregateReport, int64]
	Count         rows.Field[aggregateReport, int64]
	Present       rows.Field[aggregateReport, int64]
	Distinct      rows.Field[aggregateReport, int64]
	Total         rows.Field[aggregateReport, value.Nullable[decimal.Decimal]]
	Average       rows.Field[aggregateReport, value.Nullable[decimal.Decimal]]
	FloatTotal    rows.Field[aggregateReport, value.Nullable[float64]]
	FloatAverage  rows.Field[aggregateReport, value.Nullable[float64]]
	PeriodTotal   rows.Field[aggregateReport, value.Nullable[temporal.Interval]]
	PeriodAverage rows.Field[aggregateReport, value.Nullable[temporal.Interval]]
	Min           rows.Field[aggregateReport, value.Nullable[string]]
	Max           rows.Field[aggregateReport, value.Nullable[string]]
	Filtered      rows.Field[aggregateReport, int64]
	FilteredTotal rows.Field[aggregateReport, value.Nullable[decimal.Decimal]]
}

func TestStage6a3AggregatesGroupsAndDistinct(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE aggregate_samples (id bigint PRIMARY KEY,bucket bigint NOT NULL,required bigint NOT NULL,exact bigint,approx double precision,period interval,label text,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO aggregate_samples VALUES
 (1,1,1,9223372036854775807,2,'1 month','b',NULL),
 (2,1,2,9223372036854775807,4,'2 months','a',NULL),
 (3,2,0,NULL,NULL,NULL,NULL,NULL),
 (4,3,9,99,99,'9 months','hidden',now())`)
	must(t, err)
	table := orm.Define("aggregate_samples", func(b orm.Builder[aggregateSample], r *aggregateSample) aggregateSamples {
		return aggregateSamples{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Bucket), orm.NumberOf(b, &r.Required), orm.NullableNumberOf(b, &r.Exact), orm.NullableNumberOf(b, &r.Approx), orm.NullableIntervalOf(b, &r.Period), orm.NullableTextOf(b, &r.Label), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	schema := rows.Define(func(b rows.Builder[aggregateReport], r *aggregateReport) aggregateFields {
		return aggregateFields{b.Row(), rows.NumberOf(b, &r.Bucket), rows.NumberOf(b, &r.Count), rows.NumberOf(b, &r.Present), rows.NumberOf(b, &r.Distinct), rows.NullableNumberOf(b, &r.Total), rows.NullableNumberOf(b, &r.Average), rows.NullableNumberOf(b, &r.FloatTotal), rows.NullableNumberOf(b, &r.FloatAverage), rows.NullableFieldOf(b, &r.PeriodTotal, orm.Codec(codec.Interval())), rows.NullableFieldOf(b, &r.PeriodAverage, orm.Codec(codec.Interval())), rows.NullableTextOf(b, &r.Min), rows.NullableTextOf(b, &r.Max), rows.NumberOf(b, &r.Filtered), rows.NullableNumberOf(b, &r.FilteredTotal)}
	})
	build := func(input orm.Query[aggregateSample], grouped bool) rows.Query[rows.Alias[aggregateSide, aggregateSample], aggregateReport] {
		source := rows.As[aggregateSide](input, "s")
		scope := rows.Scope(source)
		bucket := expr.At(scope, orm.NumberExpr(table.Bucket))
		required := expr.OrderRow(expr.At(scope, orm.NumberExpr(table.Required)))
		count := expr.Count[rows.Alias[aggregateSide, aggregateSample]]()
		bucketValue := expr.Row(bucket).Value()
		if !grouped {
			bucketValue = expr.OrderRow(bucket).Param(0).Value()
		}
		q := rows.Select(source, schema,
			rows.MapSelected(schema.Bucket, bucketValue),
			rows.MapSelected(schema.Count, count.Value()),
			rows.MapSelected(schema.Present, expr.CountFieldAt(scope, orm.NullableNumberExpr(table.Exact)).Value()),
			rows.MapSelected(schema.Distinct, expr.CountDistinctFieldAt(scope, orm.NullableNumberExpr(table.Exact)).Value()),
			rows.MapSelected(schema.Total, expr.SumAt(scope, orm.NullableNumberExpr(table.Exact)).Value()),
			rows.MapSelected(schema.Average, expr.AvgAt(scope, orm.NullableNumberExpr(table.Exact)).Value()),
			rows.MapSelected(schema.FloatTotal, expr.SumFloatAt(scope, orm.NullableNumberExpr(table.Approx)).Value()),
			rows.MapSelected(schema.FloatAverage, expr.AvgFloatAt(scope, orm.NullableNumberExpr(table.Approx)).Value()),
			rows.MapSelected(schema.PeriodTotal, expr.SumIntervalAt(scope, orm.NullableIntervalExpr(table.Period)).Value()),
			rows.MapSelected(schema.PeriodAverage, expr.AvgIntervalAt(scope, orm.NullableIntervalExpr(table.Period)).Value()),
			rows.MapSelected(schema.Min, expr.MinAt(scope, orm.NullableTextExpr(table.Label)).Value()),
			rows.MapSelected(schema.Max, expr.MaxAt(scope, orm.NullableTextExpr(table.Label)).Value()),
			rows.MapSelected(schema.Filtered, count.Filter(required.Gt(1)).Value()),
			rows.MapSelected(schema.FilteredTotal, expr.SumAt(scope, orm.NullableNumberExpr(table.Exact)).Filter(required.Gt(1)).Value()),
		)
		if grouped {
			q = rows.GroupBy(q, expr.Group(bucket))
			q = rows.OrderBy(q, bucket.Asc())
		}
		return q
	}
	got, err := rows.Read(build(table.Query(), true)).All(ctx, db)
	must(t, err)
	if len(got) != 2 {
		t.Fatal("scope/group count", got)
	}
	first, last := got[0], got[1]
	if first.Bucket != 1 || first.Count != 2 || first.Present != 2 || first.Distinct != 1 || first.Filtered != 1 {
		t.Fatal(first)
	}
	if x, ok := first.Total.Get(); !ok || x.String() != "18446744073709551614" {
		t.Fatal("integer sum overflow/representation", first.Total)
	}
	if x, ok := first.Average.Get(); !ok || x.Cmp(decimal.FromInt64(9223372036854775807)) != 0 {
		t.Fatal(first.Average)
	}
	if x, ok := first.FloatTotal.Get(); !ok || x != 6 {
		t.Fatal(first.FloatTotal)
	}
	if x, ok := first.FloatAverage.Get(); !ok || x != 3 {
		t.Fatal(first.FloatAverage)
	}
	if x, ok := first.PeriodTotal.Get(); !ok || x.Months() != 3 || x.Days() != 0 {
		t.Fatal(first.PeriodTotal)
	}
	if x, ok := first.PeriodAverage.Get(); !ok || x.Months() != 1 || x.Days() != 15 {
		t.Fatal(first.PeriodAverage)
	}
	if x, ok := first.Min.Get(); !ok || x != "a" {
		t.Fatal(first.Min)
	}
	if x, ok := first.Max.Get(); !ok || x != "b" {
		t.Fatal(first.Max)
	}
	if x, ok := first.FilteredTotal.Get(); !ok || x.Cmp(decimal.FromInt64(9223372036854775807)) != 0 {
		t.Fatal(first.FilteredTotal)
	}
	if last.Count != 1 || last.Present != 0 || last.Distinct != 0 || !last.Total.IsNull() || !last.Average.IsNull() || !last.FloatTotal.IsNull() || !last.FloatAverage.IsNull() || !last.PeriodTotal.IsNull() || !last.PeriodAverage.IsNull() || !last.Min.IsNull() || !last.Max.IsNull() || !last.FilteredTotal.IsNull() || last.Filtered != 0 {
		t.Fatal("all-NULL/filtered group", last)
	}
	empty, err := rows.Read(build(table.Query().Where(orm.NumberLt(table.Bucket, 0)), false)).All(ctx, db)
	must(t, err)
	if len(empty) != 1 || empty[0].Count != 0 || empty[0].Present != 0 || empty[0].Distinct != 0 || !empty[0].Total.IsNull() || !empty[0].Average.IsNull() || !empty[0].FloatTotal.IsNull() || !empty[0].PeriodTotal.IsNull() || !empty[0].Min.IsNull() {
		t.Fatal("empty aggregate", empty)
	}
	count := expr.Count[rows.Alias[aggregateSide, aggregateSample]]()
	filtered, err := rows.Read(rows.Having(build(table.Query(), true), count.Gt(1))).All(ctx, db)
	must(t, err)
	if len(filtered) != 1 || filtered[0].Bucket != 1 {
		t.Fatal("HAVING", filtered)
	}
	modelBucket, id := expr.Value(orm.NumberExpr(table.Bucket)), expr.Value(orm.KeyExpr(table.Key()))
	distinct := rows.OrderBy(rows.DistinctOn(rows.FromModel(table.Query()), expr.Group(modelBucket)), modelBucket.Asc(), id.Desc())
	chosen, err := rows.Read(distinct).All(ctx, db)
	must(t, err)
	if len(chosen) != 2 || chosen[0].ID != 2 || chosen[1].ID != 3 {
		t.Fatal("DISTINCT ON/scopes", chosen)
	}
	values, err := rows.Read(rows.Distinct(rows.SelectValue(table.Query(), orm.NumberExpr(table.Bucket)))).All(ctx, db)
	must(t, err)
	if len(values) != 2 {
		t.Fatal("DISTINCT values", values)
	}
	// Selected aggregate keys/order remain read-only and obey grouping rules.
	single, err := rows.Read(rows.DistinctOnValues(build(table.Query().Where(orm.NumberEq(table.Bucket, 1)), false), count.Value().Key())).All(ctx, db)
	must(t, err)
	if len(single) != 1 || single[0].Count != 2 {
		t.Fatal(single)
	}
	rollback := errors.New("rollback grouped aggregate")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, e := orm.Insert(ctx, tx, table.Table, orm.KeySet(table.Key(), 5), orm.NumberSet(table.Bucket, 1), orm.NumberSet(table.Required, 1))
		if e != nil {
			return e
		}
		out, e := rows.Read(build(table.Query(), true)).All(ctx, tx)
		if e != nil {
			return e
		}
		if len(out) != 2 || out[0].Count != 3 {
			t.Fatal("aggregate transaction visibility", out)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	out, err := rows.Read(build(table.Query(), true)).All(ctx, db)
	must(t, err)
	if len(out) != 2 || out[0].Count != 2 {
		t.Fatal("aggregate rollback", out)
	}
}
