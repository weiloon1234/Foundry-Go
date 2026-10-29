package rows

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type outerRecord struct {
	ID       value.Nullable[int64]
	Nickname value.Nullable[string]
}
type outerFields struct {
	Row[outerRecord]
	ID       Field[outerRecord, value.Nullable[int64]]
	Nickname Field[outerRecord, value.Nullable[string]]
}

func outerSchema() outerFields {
	return Define(func(b Builder[outerRecord], r *outerRecord) outerFields {
		return outerFields{b.Row(), NullableNumberOf(b, &r.ID), NullableTextOf(b, &r.Nickname)}
	})
}

type correlationCount struct{ Count value.Nullable[int64] }
type correlationCountFields struct {
	Row[correlationCount]
	Count Field[correlationCount, value.Nullable[int64]]
}

func correlationCountSchema() correlationCountFields {
	return Define(func(b Builder[correlationCount], r *correlationCount) correlationCountFields {
		return correlationCountFields{b.Row(), NullableNumberOf(b, &r.Count)}
	})
}

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

type expressionRow[V any] struct{ Value V }
type expressionFields[V any] struct {
	Row[expressionRow[V]]
	Value Field[expressionRow[V], V]
}

func expressionSchema[V any](c codec.Codec[V]) expressionFields[V] {
	return Define(func(b Builder[expressionRow[V]], r *expressionRow[V]) expressionFields[V] {
		return expressionFields[V]{b.Row(), FieldOf(b, &r.Value, orm.Codec(c))}
	})
}

func expressionQuery[S, V any](source SelectionSource[S], v expr.Selected[S, V], c codec.Codec[V]) Query[S, expressionRow[V]] {
	schema := expressionSchema(c)
	return Select(source, schema, MapSelected(schema.Value, v))
}

func expressionSQL[S, R any](t *testing.T, q Query[S, R]) querycore.Statement {
	t.Helper()
	s, err := querycore.CompileRead(q.source.Node, context.Background(), false, q.source.Err, true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func SourceOf[S any](s SelectionSource[S]) ormcore.Source { return sourceOf(s) }

func QuerySource[S, R any](q *Query[S, R]) *ormcore.Source { return &q.source }

func ScalarSource[S, V any](q *Scalar[S, V]) *ormcore.Source { return &q.source }

func InputSource[S any](q *Input[S]) *ormcore.Source { return &q.source }

func CorrelatedSourceCore[O, I any](c *CorrelatedSource[O, I]) *ormcore.Correlation { return &c.core }

func CorrelatedScalarCore[O, I, V any](c *CorrelatedScalar[O, I, V]) *ormcore.Correlation {
	return &c.core
}

func CursorQueryCore[R any](c CursorQuery[R]) ormcore.ResultCursor { return c.core }

func ValueCursorQueryCore[V any](c ValueCursorQuery[V]) ormcore.ResultCursor { return c.core }

func ValueRecordCore[V any](r ValueRecord[V]) ormcore.RecordSource { return r.core }

func RecordReadCore[M, R any](r RecordRead[M, R]) ormcore.RecordSource { return r.core }

func ValueSetQueryCore[V any](s ValueSetQuery[V]) ormcore.RecordSource { return s.core }

func SetQueryCore[R any](s SetQuery[R]) ormcore.RecordSource { return s.core }
