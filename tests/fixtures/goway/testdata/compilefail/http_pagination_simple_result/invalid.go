package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = integrations.List.Handle(func(context.Context, integrations.Request) (orm.SimplePage[integrations.Response], error) {
	return orm.SimplePage[integrations.Response]{}, nil
})
