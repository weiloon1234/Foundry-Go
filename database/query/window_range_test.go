package query

import (
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestRangeDistanceCaptureAndOwnership(t *testing.T) {
	encoded := []byte("1")
	calls := 0
	c := codec.New(func(decimal.Decimal) (driver.Value, error) { calls++; return encoded, nil }, func(any) (decimal.Decimal, error) { return decimal.Decimal{}, nil }).WithParameterType(codec.TypeDecimal)
	f := NewExactField[cursorRecord, decimal.Decimal]("records", "rank", c)
	r := NumericRange(WindowFor(cursorQuery()), f.Value())
	bound := r.Preceding(decimal.FromInt64(1))
	encoded[0] = '9'
	q := SelectValue(cursorQuery(), RowNumber(r.Between(bound, r.CurrentRow())))
	for range 2 {
		s, err := q.Compile()
		if err != nil || calls != 1 || !reflect.DeepEqual(s.Arguments(), []any{[]byte("1")}) {
			t.Fatal("RANGE distance rebound or retained a mutable input", calls, s.Arguments(), err)
		}
		s.Arguments()[0].([]byte)[0] = '7'
	}
}

func TestTypedNumericRangeFrames(t *testing.T) {
	q := cursorQuery()
	id := scopedID(q.Scope())
	r := NumericRange(WindowFor(q), id.Value()).Desc()
	w := r.Between(r.Preceding(2), r.Following(3))
	s, err := SelectValue(q, Count[cursorRecord]().Over(w)).Compile()
	if err != nil || !strings.Contains(s.SQL(), `ORDER BY "records"."id" DESC RANGE BETWEEN CAST($1 AS bigint) PRECEDING AND CAST($2 AS bigint) FOLLOWING`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	for _, w := range []Window[cursorRecord]{
		r.Between(r.Preceding(-1), r.CurrentRow()),
		r.Between(r.CurrentRow(), r.Preceding(1)),
		w.OrderBy(id.Asc()),
		NumericRange(WindowFor(q).OrderBy(id.Asc()), id.Value()).Between(r.CurrentRow(), r.UnboundedFollowing()),
		(RangeWindow[cursorRecord, int64]{}).Between(r.CurrentRow(), r.CurrentRow()),
	} {
		if _, err := SelectValue(q, Count[cursorRecord]().Over(w)).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid RANGE compiled", err)
		}
	}
	f := NewFloatField[cursorRecord, float64]("records", "rank", codec.Float[float64]())
	fr := NumericRange(WindowFor(q), f.Value())
	for _, v := range []float64{-1, math.Inf(1), math.NaN()} {
		if _, err := SelectValue(q, RowNumber(fr.Between(fr.Preceding(v), fr.CurrentRow()))).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid float distance compiled", err)
		}
	}
}

func TestTemporalRangeComponentsAndSign(t *testing.T) {
	q := cursorQuery()
	f := NewOrderedField[cursorRecord, temporal.Date]("records", "rank", codec.Date())
	r := TemporalRange(WindowFor(q), f.Value())
	mixed, err := temporal.NewInterval(-1, 31, 0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := SelectValue(q, RowNumber(r.Between(r.Preceding(mixed), r.CurrentRow()))).Compile()
	if err != nil || !strings.Contains(s.SQL(), "CAST($1 AS interval) PRECEDING") || !reflect.DeepEqual(s.Arguments(), []any{mixed.String()}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	for _, v := range []temporal.Interval{temporal.Days(-1), temporal.Months(-1)} {
		if _, err := SelectValue(q, RowNumber(r.Between(r.Preceding(v), r.CurrentRow()))).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("negative interval accepted", err)
		}
	}
	clock := NewOrderedField[cursorRecord, temporal.Time]("records", "rank", codec.WallTime())
	tr := TemporalRange(WindowFor(q), clock.Value())
	if _, err := SelectValue(q, RowNumber(tr.Between(tr.Preceding(temporal.Days(1)), tr.CurrentRow()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("wall time silently ignored calendar components", err)
	}
	hour, _ := temporal.Elapsed(time.Hour)
	if _, err := SelectValue(q, RowNumber(tr.Between(tr.Preceding(hour), tr.CurrentRow()))).Compile(); err != nil {
		t.Fatal(err)
	}
}
