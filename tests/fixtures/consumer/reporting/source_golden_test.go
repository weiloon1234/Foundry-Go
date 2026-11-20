package reporting

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/datatable"
)

type reportStatement interface {
	SQL() string
	Arguments() []any
}

// reportSelectionStatement is the selection one table statement reads.
type reportSelectionStatement struct {
	sql       string
	arguments []any
}

func (s reportSelectionStatement) SQL() string      { return s.sql }
func (s reportSelectionStatement) Arguments() []any { return s.arguments }

func assertReportStatement(t *testing.T, got reportStatement) {
	t.Helper()
	if !oracleEqual(t, "consumer/reporting/source_golden_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("report source changed")
	}
}

// reportSelection compiles a table's real Query statements. The recorded
// goldens are each table's selection before sort and pagination: exactly what
// the count statement counts, and what the page statement orders and limits.
func reportSelection[S, R any](t *testing.T, table datatable.Table[S, R, Authority], authority Authority) reportSelectionStatement {
	t.Helper()
	statements, err := datatable.Compile(t.Context(), table, datatable.DefaultConfig(), authority, datatable.Request{})
	if err != nil {
		t.Fatal(err)
	}
	selection, counted := strings.CutPrefix(statements.Count.SQL(), `SELECT COUNT(*) FROM (`)
	selection, wrapped := strings.CutSuffix(selection, `) AS "foundry_count"`)
	if !counted || !wrapped {
		t.Fatalf("count statement does not count the selection: %s", statements.Count.SQL())
	}
	if !strings.HasPrefix(statements.Rows.SQL(), selection+" ORDER BY ") {
		t.Fatalf("page statement reads a different selection:\n%s\n%s", statements.Rows.SQL(), selection)
	}
	return reportSelectionStatement{sql: selection, arguments: statements.Count.Arguments()}
}

func TestReportSourceGolden(t *testing.T) {
	authority := Authority{resolved: true, actor: Operator{TenantID: 7, Active: true, CanView: true, CanExport: true}}
	t.Run("members", func(t *testing.T) {
		assertReportStatement(t, reportSelection(t, Members, authority))
	})
	t.Run("orders", func(t *testing.T) {
		assertReportStatement(t, reportSelection(t, Orders, authority))
	})
	t.Run("totals", func(t *testing.T) {
		assertReportStatement(t, reportSelection(t, Totals, authority))
	})
}
