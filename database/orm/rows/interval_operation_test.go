package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
)

func TestNestedEpochConversionsHaveLinearSQLGrowth(t *testing.T) {
	f := declareAggregateRecords()
	var v expr.RowValue[aggregateRecord, int64] = expr.Value(orm.NumberExpr(f.Exact))
	for range 30 {
		v = expr.UnixMilliseconds(expr.FromUnixMillis(v))
	}
	s, err := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Row(v).Value())))
	if err != nil || len(s.SQL()) > 16000 {
		t.Fatal("nested epoch SQL expanded", len(s.SQL()), err)
	}
}
func TestIntervalScalarExpansionHasBoundedWork(t *testing.T) {
	f := declareAggregateRecords()
	var v expr.RowValue[aggregateRecord, temporal.Interval] = expr.Value(orm.IntervalExpr(f.Period))
	for range 24 {
		v = expr.InstantDifference(expr.FromUnixMillis(expr.IntervalMonths(v)), expr.TransactionTime(ModelScope(f.Query())))
	}
	_, err := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Row(v).Value())))
	if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "scalar SQL") {
		t.Fatal("scalar expansion escaped work bound", err)
	}
}
func TestScalarSQLPlanningSharesCompilationBudget(t *testing.T) {
	f := declareAggregateRecords()
	node := expr.FoundrySelected(expr.IntervalMonths(expr.Param(orm.IntervalExpr(f.Period), temporal.Months(1))).Value(), sqlowner.Expressions()).Node
	var bytes int
	for _, tracked := range []bool{false, true} {
		var c querycore.Compiler
		if tracked {
			c.TrackShape()
		}
		if _, err := c.PlanValue(node); err != nil || c.ScalarSQLBytes == 0 {
			t.Fatal("planning discarded work", err)
		}
		if tracked && bytes != c.ScalarSQLBytes {
			t.Fatal("shape accounting differs", bytes, c.ScalarSQLBytes)
		}
		bytes = c.ScalarSQLBytes
		c.ScalarSQLBytes = querycore.MaxScalarSQLBytes - c.ScalarSQLBytes + 1
		if _, err := c.PlanValue(node); !errors.Is(err, fault.Invalid) || c.ScalarSQLBytes <= querycore.MaxScalarSQLBytes {
			t.Fatal("planning bypassed work budget", err)
		}
	}
}
func TestIntervalSQLValidationAndAggregates(t *testing.T) {
	f := declareAggregateRecords()
	q := From(f.Query())
	sum := expr.SumInterval(orm.IntervalExpr(f.Period))
	s, err := CompileRead(ReadScalar(HavingScalar(SelectScalar(q, sum.Value()), sum.Gt(temporal.Interval{}))))
	if err != nil || !strings.Contains(s.SQL(), "SUM(CAST(") || !strings.Contains(s.SQL(), "AS interval)") {
		t.Fatal(s.SQL(), err)
	}
	window := expr.WindowFor(ModelScope(f.Query())).OrderBy(expr.Value(orm.IntervalExpr(f.Period)).Asc())
	if _, err := CompileRead(ReadScalar(SelectScalar(q, expr.Over(expr.AvgInterval(orm.IntervalExpr(f.Period)).Filter(expr.ModelPredicate(orm.IntervalNe(f.Period, temporal.Interval{}))), window)))); err != nil {
		t.Fatal(err)
	}
	// Malformed AST validation is owned by the shared compiler, even with LIMIT 0.
	base := sourceOf(From(f.Query())).Node
	base.Limit = value.Set(0)
	for _, node := range []querycore.OperationNode{
		{Kind: querycore.AddIntervalsOperation, Result: codec.TypeInterval},
		{Kind: querycore.IntervalMonthsOperation, Result: codec.TypeText, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "aggregate_records", Column: "period"}, Kind: codec.TypeInterval}}},
		{Kind: querycore.InstantDifferenceOperation, Result: codec.TypeInterval, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "aggregate_records", Column: "period"}, Kind: codec.TypeInterval}, {Value: querycore.FieldRef{Table: "aggregate_records", Column: "period"}, Kind: codec.TypeInterval}}},
	} {
		base.Selections = []querycore.SelectItem{{Expression: node, Alias: "value"}}
		var c querycore.Compiler
		if _, err := c.CompileSelect(base); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid interval node compiled", err)
		}
	}
}
