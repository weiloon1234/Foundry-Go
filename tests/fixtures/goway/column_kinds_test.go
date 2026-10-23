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
	_ = coremodels.Records.Query().OrderBy(coremodels.Records.Settings.Asc(), coremodels.Records.Settings.Desc(), coremodels.Records.MaybeSettings.Asc(), coremodels.Records.MaybeSettings.Desc())
	_ = ormrows.SelectValue(coremodels.Records.Query(), coremodels.Records.Settings)
	_ = ormrows.SelectValue(coremodels.Records.Query(), coremodels.Records.MaybeSettings)
	_ = ormrows.SelectValue(coremodels.Records.Query(), coremodels.Records.Period)
	_ = ormrows.SelectValue(coremodels.Records.Query(), coremodels.Records.MaybePeriod)
	_ = models.Users.Query().Where(models.Users.ID.Eq(models.User{}.ID)).ForUpdate()
}
