package scalar_field_owner_conversion

import (
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
)

type First struct{}
type Second struct{}

var _ = orm.Column[Second, string](orm.Column[First, string]{})
