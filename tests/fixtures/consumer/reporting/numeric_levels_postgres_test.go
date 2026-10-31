package reporting_test

import (
	"testing"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func TestNumericLevelReportPreservesEnumValues(t *testing.T) {
	f := openFixture(t)
	scope := f.scope(t)
	page, err := reporting.NumericLevels.Query(scope.Context(), f.manager, reporting.FromGuard(f.guard), datatable.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatal("numeric report lost tenant or soft-delete scope", page)
	}
	for _, row := range page.Items {
		if row.Level != reporting.MemberLevel || row.WideLevel != reporting.WideMemberLevel {
			t.Fatal("numeric enum projection changed", row)
		}
	}
}
