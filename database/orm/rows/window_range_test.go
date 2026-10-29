package rows

import (
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type windowRangeRecord struct {
	ID        int64
	Amount    decimal.Decimal
	Date      temporal.Date
	MaybeDate value.Nullable[temporal.Date]
	Clock     temporal.Time
}
type windowRangeTable struct {
	orm.Table[windowRangeRecord, int64]
	Amount    orm.Column[windowRangeRecord, decimal.Decimal]
	Date      orm.Time[windowRangeRecord, temporal.Date]
	MaybeDate orm.NullableTime[windowRangeRecord, temporal.Date]
	Clock     orm.Time[windowRangeRecord, temporal.Time]
}

func TestWindowRangeTypedDistancesAndCapture(t *testing.T) {
	encoded := []byte("1")
	calls := 0
	c := codec.New(func(decimal.Decimal) (driver.Value, error) { calls++; return encoded, nil }, func(any) (decimal.Decimal, error) { return decimal.Decimal{}, nil }).WithParameterType(codec.TypeDecimal)
	d := orm.Define("ranges", func(b orm.Builder[windowRangeRecord], v *windowRangeRecord) windowRangeTable {
		return windowRangeTable{orm.TableOf(b, &v.ID), orm.ColumnOf(b, &v.Amount, orm.Codec(c)), orm.TimeOf(b, &v.Date), orm.NullableTimeOf(b, &v.MaybeDate), orm.TimeOf(b, &v.Clock)}
	})
	source := FromModel(d.Query())
	w := expr.WindowFor(ModelScope(d.Query()))
	r := expr.NumericRange(w, expr.Row(expr.Value(orm.ColumnExpr(d.Amount))).Value())
	bound := r.Preceding(decimal.FromInt64(1))
	encoded[0] = '9'
	q := expressionQuery(source, expr.RowNumber(r.Desc().Between(bound, r.CurrentRow())), codec.Signed[int64]())
	for range 2 {
		s := expressionSQL(t, q)
		if calls != 1 || !reflect.DeepEqual(s.Bindings, []any{[]byte("1")}) {
			t.Fatal(calls, s.Bindings)
		}
		s.Bindings[0].([]byte)[0] = '7'
	}
	tr := expr.TemporalRange(w, expr.Row(expr.Value(orm.TimeExpr(d.Date))).Value())
	nr := expr.NullableTemporalRange(w, expr.Row(expr.Value(orm.NullableTimeExpr(d.MaybeDate))).Value())
	for _, window := range []expr.Window[windowRangeRecord]{tr.Between(tr.Preceding(temporal.Days(2)), tr.CurrentRow()), nr.Between(nr.Preceding(temporal.Days(2)), nr.CurrentRow())} {
		s := expressionSQL(t, expressionQuery(source, expr.RowNumber(window), codec.Signed[int64]()))
		if !strings.Contains(s.SQLText, "CAST($1 AS interval) PRECEDING") || !reflect.DeepEqual(s.Bindings, []any{temporal.Days(2).String()}) {
			t.Fatal(s.SQLText, s.Bindings)
		}
	}
	cr := expr.TemporalRange(w, expr.Row(expr.Value(orm.TimeExpr(d.Clock))).Value())
	for _, window := range []expr.Window[windowRangeRecord]{tr.Between(tr.Preceding(temporal.Days(-1)), tr.CurrentRow()), tr.Between(tr.Preceding(temporal.Months(-1)), tr.CurrentRow()), cr.Between(cr.Preceding(temporal.Days(1)), cr.CurrentRow())} {
		q := expressionQuery(source, expr.RowNumber(window), codec.Signed[int64]())
		if _, err := querycore.CompileRead(q.source.Node, t.Context(), false, q.source.Err, true); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestTemporalRangeComponentsAndSign(t *testing.T) {
	d := orm.Define("dates", func(b orm.Builder[windowRangeRecord], v *windowRangeRecord) windowRangeTable {
		return windowRangeTable{Table: orm.TableOf(b, &v.ID), Amount: orm.ColumnOf(b, &v.Amount, orm.Codec(codec.Decimal())), Date: orm.TimeOf(b, &v.Date), MaybeDate: orm.NullableTimeOf(b, &v.MaybeDate), Clock: orm.TimeOf(b, &v.Clock)}
	})
	source := FromModel(d.Query())
	w := expr.WindowFor(ModelScope(d.Query()))
	r := expr.TemporalRange(w, expr.Row(expr.Value(orm.TimeExpr(d.Date))).Value())
	mixed, err := temporal.NewInterval(-1, 31, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := expressionSQL(t, expressionQuery(source, expr.RowNumber(r.Between(r.Preceding(mixed), r.CurrentRow())), codec.Signed[int64]()))
	if !strings.Contains(s.SQLText, "CAST($1 AS interval) PRECEDING") || !reflect.DeepEqual(s.Bindings, []any{mixed.String()}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	clock := expr.TemporalRange(w, expr.Row(expr.Value(orm.TimeExpr(d.Clock))).Value())
	hour, err := temporal.Elapsed(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expressionSQL(t, expressionQuery(source, expr.RowNumber(clock.Between(clock.Preceding(hour), clock.CurrentRow())), codec.Signed[int64]()))
}

func TestTypedNumericRangeFrames(t *testing.T) {
	d := declareAggregateRecords()
	q := From(d.Query())
	w := expr.WindowFor(ModelScope(d.Query()))
	id := expr.Row(expr.Value(orm.NumberExpr(d.Exact))).Value()
	r := expr.NumericRange(w, id).Desc()
	framed := r.Between(r.Preceding(2), r.Following(3))
	s, err := CompileRead(ReadScalar(SelectScalar(q, expr.Over(expr.Count[aggregateRecord](), framed))))
	if err != nil || !strings.Contains(s.SQL(), `ORDER BY "aggregate_records"."exact" DESC RANGE BETWEEN CAST($1 AS bigint) PRECEDING AND CAST($2 AS bigint) FOLLOWING`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	for _, bad := range []expr.Window[aggregateRecord]{
		r.Between(r.Preceding(-1), r.CurrentRow()), r.Between(r.CurrentRow(), r.Preceding(1)), framed.OrderBy(id.Asc()),
		expr.NumericRange(w.OrderBy(id.Asc()), id).Between(r.CurrentRow(), r.UnboundedFollowing()),
		(expr.RangeWindow[aggregateRecord, int64]{}).Between(r.CurrentRow(), r.CurrentRow()),
	} {
		if _, err := CompileRead(ReadScalar(SelectScalar(q, expr.Over(expr.Count[aggregateRecord](), bad)))); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid numeric RANGE", err)
		}
	}
	fr := expr.NumericRange(w, expr.Row(expr.Value(orm.NumberExpr(d.Approx))).Value())
	for _, v := range []float64{-1, math.Inf(1), math.NaN()} {
		if _, err := CompileRead(ReadScalar(SelectScalar(q, expr.RowNumber(fr.Between(fr.Preceding(v), fr.CurrentRow()))))); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid float distance", err)
		}
	}
}
