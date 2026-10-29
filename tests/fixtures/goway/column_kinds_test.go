package goway_test

import (
	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	ormrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"testing"
)

func TestCoreDeclarations(t *testing.T) {
	if err := orm.Check(coremodels.Records, coremodels.Durations); err != nil {
		t.Fatal(err)
	}
	_ = coremodels.Records.Query().OrderBy(orm.JSONAsc(coremodels.Records.Settings), orm.JSONDesc(coremodels.Records.Settings), orm.NullableJSONAsc(coremodels.Records.MaybeSettings), orm.NullableJSONDesc(coremodels.Records.MaybeSettings))
	_ = ormrows.SelectValue(coremodels.Records.Query(), orm.JSONExpr(coremodels.Records.Settings))
	_ = ormrows.SelectValue(coremodels.Records.Query(), orm.NullableJSONExpr(coremodels.Records.MaybeSettings))
	_ = ormrows.SelectValue(coremodels.Records.Query(), orm.IntervalExpr(coremodels.Records.Period))
	_ = ormrows.SelectValue(coremodels.Records.Query(), orm.NullableIntervalExpr(coremodels.Records.MaybePeriod))
	_ = orm.Lock(models.Users.Query().Where(orm.KeyEq(models.Users.ID, models.User{}.ID)))
}
