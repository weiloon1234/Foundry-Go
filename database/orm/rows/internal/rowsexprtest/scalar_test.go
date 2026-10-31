package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func rejectScalar[S, V any](t *testing.T, source rows.SelectionSource[S], v expr.Selected[S, V], c codec.Codec[V]) {
	t.Helper()
	state := &driverState{}
	_, err := rows.Read(expressionQuery(source, v, c)).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatalf("invalid scalar reached executor: %v, %s", err, state.sql)
	}
}
func TestScalarValidationBeforeExecution(t *testing.T) {
	d := declareAggregateRecords()
	source := rows.FromModel(d.Query().Limit(0))
	instant := expr.FromUnixMillis(expr.Value(orm.NumberExpr(d.Exact)))
	local := expr.LocalAt(instant, expr.UTCZone())
	date := expr.DateOf(local)
	clock := expr.TimeOf(local)
	rejectScalar(t, source, expr.AsSelected(expr.TruncateDate(date, 0)), codec.Date())
	rejectScalar(t, source, expr.AsSelected(expr.TruncateDate(date, expr.DateUnit(expr.TimestampHour))), codec.Date())
	rejectScalar(t, source, expr.AsSelected(expr.TruncateLocal(local, 255)), codec.LocalDateTime())
	rejectScalar(t, source, expr.AsSelected(expr.TruncateInstant(instant, expr.TimestampDay, expr.TimeZone{})), codec.DateTime())
	rejectScalar(t, source, expr.AsSelected(expr.ResolveLocal(local, expr.UTCZone(), 0)), codec.DateTime())
	rejectScalar(t, source, expr.AsSelected(expr.AddClockElapsed(clock, time.Nanosecond)), codec.WallTime())
	rejectScalar(t, source, expr.AsSelected(expr.Year[aggregateRecord, temporal.Date](nil)), codec.Signed[int64]())
	rejectScalar(t, source, expr.AsSelected(expr.TransactionTime(expr.Scope[aggregateRecord, aggregateRecord]{})), codec.DateTime())
	rejectScalar(t, source, expr.AsSelected(expr.Substring(expr.Value(orm.TextExpr(d.Name)), 1, -1)), codec.String[string]())
	rejectScalar(t, source, expr.AsSelected(expr.SubstringNullable(expr.Value(orm.NullableTextExpr(d.MaybeName)), 1, -1)), codec.Nullable(codec.String[string]()))
	for _, zone := range []string{"", "Local", "../etc/passwd", "not/a/timezone", strings.Repeat("x", 256)} {
		if _, err := expr.LoadTimeZone(zone); !errors.Is(err, fault.Invalid) {
			t.Fatal(zone, err)
		}
	}
	for _, coords := range [][2]float64{{math.NaN(), 0}, {0, math.Inf(1)}, {91, 0}, {0, -181}} {
		if _, err := expr.NewGeographicPoint(coords[0], coords[1]); !errors.Is(err, fault.Invalid) {
			t.Fatal(coords, err)
		}
	}
	zone, err := expr.LoadTimeZone("America/New_York")
	if err != nil || zone.String() != "America/New_York" || expr.UTCZone().String() != "UTC" {
		t.Fatal(zone, err)
	}
}
func TestScalarRuntimeOwnershipAndPhase(t *testing.T) {
	d, foreign := declareAggregateRecords(), declareAggregateRecords()
	source := rows.FromModel(d.Query())
	rejectScalar(t, source, expr.AsSelected(expr.Lower(expr.Value(orm.TextExpr(foreign.Name)))), codec.String[string]())
	rejectScalar(t, source, expr.AsSelected(expr.Year(expr.LocalAt(expr.FromUnixMillis(expr.Value(orm.NumberExpr(foreign.Exact))), expr.UTCZone()))), codec.Signed[int64]())
	rejectScalar(t, source, expr.AsSelected(expr.AddIntervals(expr.Value(orm.IntervalExpr(d.Period)), expr.Value(orm.IntervalExpr(foreign.Period)))), codec.Interval())
	rejectScalar(t, source, expr.AsSelected(expr.SphericalDistance(expr.Value(orm.NumberExpr(d.Approx)), expr.Value(orm.NumberExpr(foreign.Approx)), expr.GeographicPoint{})), codec.Float[float64]())
	rejectScalar(t, source, expr.AsSelected(expr.TransactionTime(rows.ModelScope(foreign.Query()))), codec.DateTime())
	// A rejected scoped operand must remain rejected after both calculation and Param.
	bad := expr.At(rows.ModelScope(d.Query()), orm.TextExpr(foreign.Name))
	rejectScalar(t, source, expr.AsSelected(expr.ParamOf(expr.Lower(bad), "x")), codec.String[string]())
	w := expr.WindowFor(rows.ModelScope(d.Query()))
	windowTime := expr.FromUnixMillisValue(expr.RowNumber(w))
	q := rows.Having(expressionQuery(source, expr.AsSelected(expr.Count[aggregateRecord]()), codec.Signed[int64]()), expr.GtValue(expr.OrderValue(expr.UnixMillisecondsValue(windowTime)), 0))
	state := &driverState{}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("window hidden in HAVING", err, state.sql)
	}
	// Nullable aggregate input is still legal in a selected calculation.
	expressionSQL(t, rows.Having(expressionQuery(source, expr.LowerNullableValue(expr.AsSelected(expr.Min(orm.TextExpr(d.Name)))), codec.Nullable(codec.String[string]())), expr.TextIContainsValue(expr.TextNullableValue(expr.LowerNullableValue(expr.AsSelected(expr.Min(orm.TextExpr(d.Name))))), "_%")))
}
func TestScalarTextPredicatesAndComposition(t *testing.T) {
	d := declareAggregateRecords()
	source := rows.FromModel(d.Query())
	name := expr.Lower(expr.Value(orm.TextExpr(d.Name)))
	maybe := expr.LowerNullable(expr.Value(orm.NullableTextExpr(d.MaybeName)))
	predicates := []expr.Predicate[aggregateRecord]{expr.TextLike(name, "a%"), expr.TextContains(name, "a_%"), expr.TextIContains(name, "a_%"), expr.TextStartsWith(name, "a_%"), expr.TextEndsWith(name, "a_%"), expr.TextIStartsWith(name, "a_%"), expr.TextIEndsWith(name, "a_%"), expr.TextILike(name, "a%"), expr.NullableTextLike(maybe, "a%"), expr.NullableTextContains(maybe, "a_%"), expr.NullableTextIContains(maybe, "a_%"), expr.NullableTextStartsWith(maybe, "a_%"), expr.NullableTextEndsWith(maybe, "a_%"), expr.NullableTextIStartsWith(maybe, "a_%"), expr.NullableTextIEndsWith(maybe, "a_%"), expr.NullableTextILike(maybe, "a%")}
	for _, p := range predicates {
		s := expressionSQL(t, expressionQuery(source, expr.AsSelected(name), codec.String[string]()).Where(p))
		if !strings.Contains(s.SQLText, "LIKE") || len(s.Bindings) != 1 {
			t.Fatal(s)
		}
	}
	group := rows.GroupBy(expressionQuery(source, expr.AsSelected(name), codec.String[string]()), expr.Group(name))
	q := rows.Having(group, expr.TextLikeValue(expr.TextValue(expr.AsSelected(name)), "a%"))
	expressionSQL(t, rows.OrderBy(q, expr.AscValue(expr.AsSelected(name))))
	// Selected phase retains windows while text/nullability conversions remain explicit.
	e := expr.LowerNullableValue(expr.FirstValue(expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(d.Name)))), expr.WindowFor(rows.ModelScope(d.Query()))))
	expressionSQL(t, expressionQuery(source, e, codec.Nullable(codec.String[string]())))
}
