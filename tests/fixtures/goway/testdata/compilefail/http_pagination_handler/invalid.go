package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = pagination.Handle(integrations.List, func(context.Context, integrations.Request) (orm.PageResult[int], error) {
	return orm.PageResult[int]{}, nil
})
