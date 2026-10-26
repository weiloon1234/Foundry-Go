package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type first struct{}
type second struct{}

var _ = orm.Predicate[second](orm.Predicate[first]{})
