package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

type GroupCode string
type Group struct{ Code GroupCode }

var groups = orm.Define("groups", func(b orm.Builder[Group], m *Group) orm.Table[Group, GroupCode] {
	return orm.TableOf(b, &m.Code)
})
var identity = orm.IdentityOf(groups, groups.Key())
var wrong model.ID[Group] = identity.ReferenceOf(Group{}).Key()
