package rows

import (
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
	r := expr.NumericRange(w, expr.Select(expr.Value(d.Amount)))
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
	tr := expr.TemporalRange(w, expr.Select(expr.Value(d.Date)))
	nr := expr.NullableTemporalRange(w, expr.Select(expr.Value(d.MaybeDate)))
	for _, window := range []expr.Window[windowRangeRecord]{tr.Between(tr.Preceding(temporal.Days(2)), tr.CurrentRow()), nr.Between(nr.Preceding(temporal.Days(2)), nr.CurrentRow())} {
		s := expressionSQL(t, expressionQuery(source, expr.RowNumber(window), codec.Signed[int64]()))
		if !strings.Contains(s.SQLText, "CAST($1 AS interval) PRECEDING") || !reflect.DeepEqual(s.Bindings, []any{temporal.Days(2).String()}) {
			t.Fatal(s.SQLText, s.Bindings)
		}
	}
	cr := expr.TemporalRange(w, expr.Select(expr.Value(d.Clock)))
	for _, window := range []expr.Window[windowRangeRecord]{tr.Between(tr.Preceding(temporal.Days(-1)), tr.CurrentRow()), cr.Between(cr.Preceding(temporal.Days(1)), cr.CurrentRow())} {
		q := expressionQuery(source, expr.RowNumber(window), codec.Signed[int64]())
		if _, err := querycore.CompileRead(q.source.Node, t.Context(), false, q.source.Err, true); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
