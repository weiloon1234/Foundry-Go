package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = pagination.Handle(integrations.List, func(context.Context, integrations.Request) (orm.SimplePage[integrations.Response], error) {
	return orm.SimplePage[integrations.Response]{}, nil
})
