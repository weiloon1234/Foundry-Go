package officequeries_test

import (
	"strings"
	"testing"

	"foundry.test/consumer/officequeries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestRelationExistenceGlobalScopeCompiles(t *testing.T) {
	q := officequeries.Offices.Query().WithTrashed().Where(orm.Has(officequeries.OfficeLinks.Desks))
	for range 2 {
		statement, err := orm.Compile(orm.WithScopeContext(q, t.Context()))
		if err != nil {
			t.Fatal(err)
		}
		sql := statement.SQL()
		if strings.Count(sql, "EXISTS(") != 2 || !strings.Contains(sql, `"foundry_related_2"."id" = "foundry_related"."office_id"`) || !strings.Contains(sql, `"foundry_related_2"."deleted_at" IS NULL`) {
			t.Fatal("nested office scope lost its correlation or soft-delete filter", sql)
		}
	}
}
