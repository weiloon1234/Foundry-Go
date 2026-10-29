package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type relativeTimeRecord struct {
	ID int64
	At temporal.DateTime
}
type relativeTimeFields struct {
	orm.Table[relativeTimeRecord, int64]
	At orm.Time[relativeTimeRecord, temporal.DateTime]
}

func TestRelativeTimeWindowBindsStableValues(t *testing.T) {
	m := orm.Define("timed_records", func(b orm.Builder[relativeTimeRecord], r *relativeTimeRecord) relativeTimeFields {
		return relativeTimeFields{orm.TableOf(b, &r.ID), orm.TimeOf(b, &r.At)}
	})
	elapsed, err := temporal.Elapsed(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	compile := func() orm.Statement {
		since := expr.SubtractInstantInterval(expr.TransactionTime(rows.ModelScope(m.Query())), elapsed, expr.UTCZone())
		q := rows.WhereScalar(rows.SelectScalar(rows.From(m.Query()), expr.Row(expr.Value(orm.TimeExpr(m.At))).Value()), expr.Greater(expr.Value(orm.TimeExpr(m.At)), since))
		s, err := rows.CompileRead(rows.ReadScalar(q))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first, second := compile(), compile()
	if first.SQL() != second.SQL() || !reflect.DeepEqual(first.Arguments(), second.Arguments()) {
		t.Fatal("relative window changed between requests")
	}
	if !strings.Contains(first.SQL(), "CURRENT_TIMESTAMP") && !strings.Contains(first.SQL(), "TRANSACTION_TIMESTAMP()") {
		t.Fatal("lost transaction-relative clock", first.SQL())
	}
	if len(first.Arguments()) == 0 {
		t.Fatal("interval was not bound")
	}
}
