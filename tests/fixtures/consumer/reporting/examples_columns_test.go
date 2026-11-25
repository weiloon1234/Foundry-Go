package reporting

import (
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/datatable"
)

func exampleAuthority() Authority {
	return Authority{resolved: true, actor: Operator{TenantID: 7, Active: true, CanView: true, CanExport: true}}
}

// TestMemberSearchSQL pins the statements a request compiles to. It is the
// pattern the datatable guide shows for testing a table's SQL.
func TestMemberSearchSQL(t *testing.T) {
	statements, err := CompileMemberSearch(t.Context(), exampleAuthority())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statements.Rows.SQL(), `ILIKE $2 ESCAPE '!'`) || !slices.Contains(statements.Rows.Arguments(), any("%Ada%")) {
		t.Fatalf("search did not reach the page statement: %s %v", statements.Rows.SQL(), statements.Rows.Arguments())
	}
	if !strings.HasPrefix(statements.Count.SQL(), "SELECT COUNT(*)") {
		t.Fatal(statements.Count.SQL())
	}
}

func TestOrderLineColumnsCompile(t *testing.T) {
	table := datatable.Define(orderLineSpec())
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	statements, err := datatable.Compile(t.Context(), table, datatable.DefaultConfig(), exampleAuthority(), datatable.Request{
		Sort: []datatable.Sort{{Column: "scope", Direction: datatable.Descending}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := statements.Rows.SQL()
	for _, fragment := range []string{
		`"orders"."member_id" AS "member_id"`,                         // HiddenSelected on a joined expression
		`CAST(CHAR_LENGTH(CAST("members"."name" AS text)) AS bigint)`, // ScanWith column's selection
		`CAST($1 AS text) AS "scope"`,                                 // dynamic column
		`ORDER BY "members"."name" DESC`,                              // dynamic column sorts by its static expression
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("missing %s in %s", fragment, sql)
		}
	}
	if !slices.Contains(statements.Rows.Arguments(), any("tenant-7")) {
		t.Fatalf("dynamic value missing from %v", statements.Rows.Arguments())
	}
	if _, err := datatable.Compile(t.Context(), table, datatable.DefaultConfig(), Authority{resolved: true, actor: Operator{TenantID: 7}}, datatable.Request{}); err == nil {
		t.Fatal("Compile skipped authorization")
	}
}

func TestMemberNamesAreDistinct(t *testing.T) {
	table := datatable.Define(memberNameSpec())
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	statements, err := datatable.Compile(t.Context(), table, datatable.DefaultConfig(), exampleAuthority(), datatable.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(statements.Rows.SQL(), `SELECT DISTINCT "report_members"."name" AS "name" FROM`) {
		t.Fatal(statements.Rows.SQL())
	}
}
