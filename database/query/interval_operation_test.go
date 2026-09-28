package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestIntervalSQLValidationAndAggregates(t *testing.T) {
	f := NewIntervalField[cursorRecord, temporal.Interval]("records", "rank", codec.Interval())
	q := cursorQuery()
	for _, node := range []valueExpression{
		operationNode{kind: addIntervalsOperation, result: codec.TypeInterval},
		operationNode{kind: intervalMonthsOperation, result: codec.TypeText, arguments: []operationArgument{operationArg(f.Value())}},
		operationNode{kind: instantDifferenceOperation, result: codec.TypeInterval, arguments: []operationArgument{operationArg(f.Value()), operationArg(f.Value())}},
	} {
		if _, err := SelectValue(q.Limit(0), Expression[cursorRecord, temporal.Interval]{node: node, codec: codec.Interval()}).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if statement, err := SelectValue(q, f.Sum().Value()).Having(f.Sum().Gt(temporal.Interval{})).Compile(); err != nil || !strings.Contains(statement.SQL(), "SUM(CAST(") || !strings.Contains(statement.SQL(), "AS interval)") {
		t.Fatal(statement.SQL(), err)
	}
	window := WindowFor(q).OrderBy(f.Asc())
	if _, err := SelectValue(q, f.Avg().Filter(f.Ne(temporal.Interval{})).Over(window)).Compile(); err != nil {
		t.Fatal(err)
	}
}

func TestNestedEpochConversionsHaveLinearSQLGrowth(t *testing.T) {
	f := NewExactField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	var expr RowValue[cursorRecord, int64] = f
	for range 30 {
		expr = UnixMilliseconds(FromUnixMillis(expr))
	}
	statement, err := SelectValue(cursorQuery(), rowInput(expr).Value()).Compile()
	if err != nil || len(statement.SQL()) > 16000 {
		t.Fatal("nested epoch SQL expanded unexpectedly", len(statement.SQL()), err)
	}
}

func TestIntervalScalarExpansionHasBoundedWork(t *testing.T) {
	f := NewIntervalField[cursorRecord, temporal.Interval]("records", "rank", codec.Interval())
	var expr RowValue[cursorRecord, temporal.Interval] = f
	for range 24 {
		expr = InstantDifference(FromUnixMillis(IntervalMonths(expr)), TransactionTime(cursorQuery()))
	}
	if _, err := SelectValue(cursorQuery(), rowInput(expr).Value()).Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "scalar SQL") {
		t.Fatal("scalar expansion did not stop at its SQL work bound", err)
	}
}

func TestScalarSQLPlanningSharesCompilationBudget(t *testing.T) {
	f := NewIntervalField[cursorRecord, temporal.Interval]("records", "rank", codec.Interval())
	expr := IntervalMonths(f.Param(temporal.Months(1))).Value().node
	var c compiler
	if _, err := c.planValue(expr); err != nil || c.scalarSQLBytes == 0 {
		t.Fatal("planning discarded scalar compilation work", err)
	}
	c.scalarSQLBytes = MaxScalarSQLBytes - c.scalarSQLBytes + 1
	if _, err := c.planValue(expr); !errors.Is(err, fault.Invalid) || c.scalarSQLBytes <= MaxScalarSQLBytes {
		t.Fatal("repeated planning bypassed scalar work budget", err)
	}
}
