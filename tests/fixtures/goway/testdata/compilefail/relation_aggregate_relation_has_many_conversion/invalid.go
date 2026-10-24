package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type scope struct{}

func escape(source orm.AggregateRelation[scope, scope]) {
	_ = orm.HasMany[scope, scope](source)
}
