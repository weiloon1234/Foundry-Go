package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type first struct{}
type second struct{}

var _ = orm.Assignment[second](orm.Assignment[first]{})
