package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func invalid() {
	f, _ := factory.Define(a.Articles.Table, func(context.Context, factory.Sequence) ([]orm.Assignment[a.Article], error) { return nil, nil })
	var state factory.State[[]orm.Assignment[a.Author]]
	_, _ = f.WithStates(state)
}
