package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

func wrong() {
	_, _ = pagination.MapPage[models.User, integrations.Response](context.Background(), orm.PageResult[models.Order]{}, integrations.Present)
}
