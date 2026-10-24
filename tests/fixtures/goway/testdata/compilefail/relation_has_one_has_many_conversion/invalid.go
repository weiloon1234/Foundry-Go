package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type scope struct{}

func escape(source orm.HasOne[scope, scope]) {
	_ = orm.HasMany[scope, scope](source)
}
