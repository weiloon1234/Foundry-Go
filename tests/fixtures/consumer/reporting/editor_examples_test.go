package reporting

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/datatable"
)

func TestCompileMemberReportExample(t *testing.T) {
	authority := Authority{resolved: true, actor: Operator{TenantID: 7, Active: true, CanView: true, CanExport: true}}
	statements, err := datatable.Compile(t.Context(), Members, datatable.DefaultConfig(), authority, datatable.Request{Search: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if statements.Rows.SQL() == "" || statements.Count.SQL() == "" {
		t.Fatal("member report compiled no statement")
	}
}
