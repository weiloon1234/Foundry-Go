package reporting

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type reportStatement interface {
	SQL() string
	Arguments() []any
}

func assertReportStatement(t *testing.T, got reportStatement) {
	t.Helper()
	if !oracleEqual(t, "consumer/reporting/source_golden_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("report source changed")
	}
}

func TestReportSourceGolden(t *testing.T) {
	authority := Authority{resolved: true, actor: Operator{TenantID: 7, Active: true, CanView: true, CanExport: true}}
	t.Run("members", func(t *testing.T) {
		source, err := memberSpec().Source(t.Context(), authority)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		assertReportStatement(t, got)
	})
	t.Run("orders", func(t *testing.T) {
		source, err := orderSpec().Source(t.Context(), authority)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		assertReportStatement(t, got)
	})
	t.Run("totals", func(t *testing.T) {
		source, err := totalSpec().Source(t.Context(), authority)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		assertReportStatement(t, got)
	})
}
