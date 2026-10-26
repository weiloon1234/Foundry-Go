package reporting

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestCompileMemberReportExample(t *testing.T) {
	authority := Authority{resolved: true, actor: Operator{TenantID: 7, Active: true, CanView: true, CanExport: true}}
	source, err := memberSpec().Source(t.Context(), authority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rows.CompileRead(rows.Read(source)); err != nil {
		t.Fatal(err)
	}
}
