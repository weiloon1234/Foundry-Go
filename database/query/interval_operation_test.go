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
		operationNode{Kind: addIntervalsOperation, Result: codec.TypeInterval},
		operationNode{Kind: intervalMonthsOperation, Result: codec.TypeText, Arguments: []operationArgument{operationArg(Value(f))}},
		operationNode{Kind: instantDifferenceOperation, Result: codec.TypeInterval, Arguments: []operationArgument{operationArg(Value(f)), operationArg(Value(f))}},
	} {
		if _, err := SelectValue(q.Limit(0), Expression[cursorRecord, temporal.Interval]{node: node, codec: codec.Interval()}).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if statement, err := SelectValue(q, Sum(f).Value()).Having(Sum(f).Gt(temporal.Interval{})).Compile(); err != nil || !strings.Contains(statement.SQL(), "SUM(CAST(") || !strings.Contains(statement.SQL(), "AS interval)") {
		t.Fatal(statement.SQL(), err)
	}
	window := WindowFor(q).OrderBy(f.Asc())
	if _, err := SelectValue(q, Avg(f).Filter(f.Ne(temporal.Interval{})).Over(window)).Compile(); err != nil {
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
	expr := IntervalMonths(Param(f, temporal.Months(1))).Value().node
	var bytes int
	for _, tracked := range []bool{false, true} {
		var c compiler
		if tracked {
			c.TrackShape()
		}
		if _, err := c.PlanValue(expr); err != nil || c.ScalarSQLBytes == 0 {
			t.Fatal("planning discarded scalar compilation work", err)
		}
		if tracked && bytes != c.ScalarSQLBytes {
			t.Fatal("shape fragments changed SQL work accounting", bytes, c.ScalarSQLBytes)
		}
		bytes = c.ScalarSQLBytes
		c.ScalarSQLBytes = MaxScalarSQLBytes - c.ScalarSQLBytes + 1
		if _, err := c.PlanValue(expr); !errors.Is(err, fault.Invalid) || c.ScalarSQLBytes <= MaxScalarSQLBytes {
			t.Fatal("repeated planning bypassed scalar work budget", err)
		}
	}
}
