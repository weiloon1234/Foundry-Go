package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"strings"
	"testing"
	"time"
)

type temporalTransferRow struct {
	ID    int64
	Day   temporal.Date
	Local temporal.LocalDateTime
	At    time.Time
	Clock temporal.Time
}
type temporalTransferTable struct {
	orm.Table[temporalTransferRow, int64]
	Day   orm.Time[temporalTransferRow, temporal.Date]
	Local orm.Time[temporalTransferRow, temporal.LocalDateTime]
	At    orm.Time[temporalTransferRow, time.Time]
	Clock orm.Time[temporalTransferRow, temporal.Time]
}

func temporalTransferFields() temporalTransferTable {
	return orm.Define("temporal_records", func(b orm.Builder[temporalTransferRow], m *temporalTransferRow) temporalTransferTable {
		return temporalTransferTable{orm.TableOf(b, &m.ID), orm.TimeOf(b, &m.Day), orm.TimeOf(b, &m.Local), orm.TimeOf(b, &m.At), orm.TimeOf(b, &m.Clock)}
	})
}
func temporalTransferNode[S, V any](s expr.Selected[S, V]) querycore.ValueExpression {
	return expr.FoundrySelected(s, sqlowner.Expressions()).Node
}
func TestTemporalOperationsValidateBeforeZeroLimit(t *testing.T) {
	d := temporalTransferFields()
	date, local, at, clock := expr.Value(orm.TimeExpr(d.Day)), expr.Value(orm.TimeExpr(d.Local)), expr.Value(orm.TimeExpr(d.At)), expr.Value(orm.TimeExpr(d.Clock))
	rejectTemporalTransfer(t, d, "date unit zero", expr.AsSelected(expr.TruncateDate(date, 0)))
	rejectTemporalTransfer(t, d, "date clock unit", expr.AsSelected(expr.TruncateDate(date, expr.DateUnit(expr.TimestampHour))))
	rejectTemporalTransfer(t, d, "timestamp unit", expr.AsSelected(expr.TruncateLocal(local, 255)))
	rejectTemporalTransfer(t, d, "zone zero", expr.AsSelected(expr.TruncateInstant(at, expr.TimestampDay, expr.TimeZone{})))
	rejectTemporalTransfer(t, d, "resolution policy", expr.AsSelected(expr.ResolveLocal(local, expr.UTCZone(), 0)))
	rejectTemporalTransfer(t, d, "submicrosecond", expr.AsSelected(expr.AddClockElapsed(clock, time.Nanosecond)))
	rejectTemporalTransfer(t, d, "nil row", expr.AsSelected(expr.Year[temporalTransferRow, temporal.Date](nil)))
	rejectTemporalTransfer(t, d, "nil scope", expr.AsSelected(expr.TransactionTime(expr.Scope[temporalTransferRow, temporalTransferRow]{})))
	for name, v := range map[string]querycore.ValueExpression{
		"wrong representation":   querycore.OperationNode{Kind: querycore.ExtractYearOperation, Result: codec.TypeInteger, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "temporal_records", Column: "day"}, Kind: codec.TypeText}}},
		"wrong result":           querycore.OperationNode{Kind: querycore.ExtractYearOperation, Result: codec.TypeText, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "temporal_records", Column: "day"}, Kind: codec.TypeDate}}},
		"wrong extraction input": querycore.OperationNode{Kind: querycore.ExtractYearOperation, Result: codec.TypeInteger, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "temporal_records", Column: "at"}, Kind: codec.TypeDateTime}}}, "missing arguments": querycore.OperationNode{Kind: querycore.TruncateInstantOperation, Result: codec.TypeDateTime},
	} {
		n := rows.SourceOf(rows.From(d.Query().Limit(0))).Node
		n.Selections = []querycore.SelectItem{{Expression: v}}
		if _, e := querycore.CompileRead(n, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
			t.Fatal(name, e)
		}
	}
	for _, name := range []string{"", "Local", "../etc/passwd", "not/a/timezone", strings.Repeat("a", 256)} {
		if _, e := expr.LoadTimeZone(name); !errors.Is(e, fault.Invalid) {
			t.Fatal(name, e)
		}
	}
	zone, e := expr.LoadTimeZone("America/New_York")
	if e != nil || zone.String() != "America/New_York" || expr.UTCZone().String() != "UTC" {
		t.Fatal(zone, e)
	}
}
func TestTemporalCompilerRetainsScopePhaseAndParameters(t *testing.T) {
	d := temporalTransferFields()
	year := expr.Year(expr.LocalAt(expr.Value(orm.TimeExpr(d.At)), expr.UTCZone()))
	q := rows.OrderScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(year)), expr.Group(year)), expr.Asc(year))
	s, e := rows.CompileRead(rows.ReadScalar(q))
	if e != nil || !strings.Contains(s.SQL(), "EXTRACT(year FROM") || !strings.Contains(s.SQL(), "TIMEZONE(") {
		t.Fatal(s.SQL(), e)
	}
	for _, a := range s.Arguments() {
		if a != "UTC" {
			t.Fatal(s.Arguments())
		}
	}
	original := temporalTransferNode(expr.AsSelected(year))
	renamed := querycore.RequalifyValue(original, "qualified").(querycore.OperationNode)
	if renamed.Arguments[0].Value.(querycore.OperationNode).Arguments[1].Value.(querycore.FieldRef).Table != "qualified" || original.(querycore.OperationNode).Arguments[0].Value.(querycore.OperationNode).Arguments[1].Value.(querycore.FieldRef).Table != "temporal_records" {
		t.Fatal("temporal qualifier mutation")
	}
	window := expr.FromUnixMillisValue(expr.RowNumber(expr.WindowFor(rows.ModelScope(d.Query()))))
	p := expr.GtValue(expr.OrderValue(expr.UnixMillisecondsValue(window)), 0)
	if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Count[temporalTransferRow]())), p))); !errors.Is(e, fault.Invalid) {
		t.Fatal("window hidden in HAVING", e)
	}
	v := expr.UnixMillisecondsNullableValue(expr.AsSelected(expr.Min(orm.TimeExpr(d.At))))
	if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(rows.From(d.Query()), v), expr.GtValue(expr.OrderNullableValue(v), 0)))); e != nil {
		t.Fatal("aggregate phase lost", e)
	}
}

func rejectTemporalTransfer[V any](t *testing.T, d temporalTransferTable, name string, v expr.Selected[temporalTransferRow, V]) {
	t.Helper()
	s, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(d.Query().Limit(0)), v)))
	if !errors.Is(e, fault.Invalid) || s.SQL() != "" {
		t.Fatal(name, s.SQL(), e)
	}
}
