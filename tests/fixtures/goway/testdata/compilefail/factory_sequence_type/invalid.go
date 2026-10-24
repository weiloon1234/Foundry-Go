package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

var _, _ = factory.Define(a.Articles.Table, func(context.Context, string) ([]orm.Assignment[a.Article], error) { return nil, nil })
