package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type first struct{}
type second struct{}

var _ = orm.GlobalScope[second](orm.Scope(orm.Table[first, int]{}, "visible", func(context.Context) (orm.Predicate[first], error) { return orm.Predicate[first]{}, nil }))
