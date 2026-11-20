package invalid

import (
	"foundry.test/goway/models"
	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func f(h auditrecord.Model[models.User, model.ID[models.User]]) {
	_, _ = orm.AuditReadNullable(h, models.Users.Email)
}
