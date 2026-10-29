package rows

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

func rejectScalar[S, V any](t *testing.T, source SelectionSource[S], v expr.Selected[S, V], c codec.Codec[V]) {
	t.Helper()
	state := &driverState{}
	_, err := Read(expressionQuery(source, v, c)).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatalf("invalid scalar reached executor: %v, %s", err, state.sql)
	}
}
func TestScalarValidationBeforeExecution(t *testing.T) {
	d := declareAggregateRecords()
	source := FromModel(d.Query().Limit(0))
	instant := expr.FromUnixMillis(expr.Value(orm.NumberExpr(d.Exact)))
	local := expr.LocalAt(instant, expr.UTCZone())
	date := expr.DateOf(local)
	clock := expr.TimeOf(local)
	rejectScalar(t, source, expr.TruncateDate(date, 0).Value(), codec.Date())
	rejectScalar(t, source, expr.TruncateDate(date, expr.DateUnit(expr.TimestampHour)).Value(), codec.Date())
	rejectScalar(t, source, expr.TruncateLocal(local, 255).Value(), codec.LocalDateTime())
	rejectScalar(t, source, expr.TruncateInstant(instant, expr.TimestampDay, expr.TimeZone{}).Value(), codec.DateTime())
	rejectScalar(t, source, expr.ResolveLocal(local, expr.UTCZone(), 0).Value(), codec.DateTime())
	rejectScalar(t, source, expr.AddClockElapsed(clock, time.Nanosecond).Value(), codec.WallTime())
	rejectScalar(t, source, expr.Year[aggregateRecord, temporal.Date](nil).Value(), codec.Signed[int64]())
	rejectScalar(t, source, expr.TransactionTime(expr.Scope[aggregateRecord, aggregateRecord]{}).Value(), codec.DateTime())
	rejectScalar(t, source, expr.Substring(expr.Value(orm.TextExpr(d.Name)), 1, -1).Value(), codec.String[string]())
	rejectScalar(t, source, expr.SubstringNullable(expr.Value(orm.NullableTextExpr(d.MaybeName)), 1, -1).Value(), codec.Nullable(codec.String[string]()))
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
	source := FromModel(d.Query())
	rejectScalar(t, source, expr.Lower(expr.Value(orm.TextExpr(foreign.Name))).Value(), codec.String[string]())
	rejectScalar(t, source, expr.Year(expr.LocalAt(expr.FromUnixMillis(expr.Value(orm.NumberExpr(foreign.Exact))), expr.UTCZone())).Value(), codec.Signed[int64]())
	rejectScalar(t, source, expr.AddIntervals(expr.Value(orm.IntervalExpr(d.Period)), expr.Value(orm.IntervalExpr(foreign.Period))).Value(), codec.Interval())
	rejectScalar(t, source, expr.SphericalDistance(expr.Value(orm.NumberExpr(d.Approx)), expr.Value(orm.NumberExpr(foreign.Approx)), expr.GeographicPoint{}).Value(), codec.Float[float64]())
	rejectScalar(t, source, expr.TransactionTime(ModelScope(foreign.Query())).Value(), codec.DateTime())
	// A rejected scoped operand must remain rejected after both calculation and Param.
	bad := expr.At(ModelScope(d.Query()), orm.TextExpr(foreign.Name))
	rejectScalar(t, source, expr.Lower(bad).Param("x").Value(), codec.String[string]())
	w := expr.WindowFor(ModelScope(d.Query()))
	windowTime := expr.FromUnixMillisValue(expr.RowNumber(w))
	q := Having(expressionQuery(source, expr.Count[aggregateRecord]().Value(), codec.Signed[int64]()), expr.OrderValue(expr.UnixMillisecondsValue(windowTime)).Gt(0))
	state := &driverState{}
	if _, err := Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("window hidden in HAVING", err, state.sql)
	}
	// Nullable aggregate input is still legal in a selected calculation.
	expressionSQL(t, Having(expressionQuery(source, expr.LowerNullableValue(expr.Min(orm.TextExpr(d.Name)).Value()), codec.Nullable(codec.String[string]())), expr.TextNullableValue(expr.LowerNullableValue(expr.Min(orm.TextExpr(d.Name)).Value())).IContains("_%")))
}
func TestScalarTextPredicatesAndComposition(t *testing.T) {
	d := declareAggregateRecords()
	source := FromModel(d.Query())
	name := expr.Lower(expr.Value(orm.TextExpr(d.Name)))
	maybe := expr.LowerNullable(expr.Value(orm.NullableTextExpr(d.MaybeName)))
	predicates := []expr.Predicate[aggregateRecord]{name.Like("a%"), name.Contains("a_%"), name.IContains("a_%"), name.StartsWith("a_%"), name.EndsWith("a_%"), name.IStartsWith("a_%"), name.IEndsWith("a_%"), name.ILike("a%"), maybe.Like("a%"), maybe.Contains("a_%"), maybe.IContains("a_%"), maybe.StartsWith("a_%"), maybe.EndsWith("a_%"), maybe.IStartsWith("a_%"), maybe.IEndsWith("a_%"), maybe.ILike("a%")}
	for _, p := range predicates {
		s := expressionSQL(t, expressionQuery(source, name.Value(), codec.String[string]()).Where(p))
		if !strings.Contains(s.SQLText, "LIKE") || len(s.Bindings) != 1 {
			t.Fatal(s)
		}
	}
	group := GroupBy(expressionQuery(source, name.Value(), codec.String[string]()), expr.Group(name))
	q := Having(group, expr.TextValue(name.Value()).Like("a%"))
	expressionSQL(t, OrderBy(q, name.Value().Asc()))
	// Selected phase retains windows while text/nullability conversions remain explicit.
	e := expr.LowerNullableValue(expr.FirstValue(expr.Row(expr.Value(orm.TextExpr(d.Name))).Value(), expr.WindowFor(ModelScope(d.Query()))))
	expressionSQL(t, expressionQuery(source, e, codec.Nullable(codec.String[string]())))
}
