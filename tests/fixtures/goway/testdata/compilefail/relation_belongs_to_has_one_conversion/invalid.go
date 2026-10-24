package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type scope struct{}

func escape(source orm.BelongsTo[scope, scope]) {
	_ = orm.HasOne[scope, scope](source)
}
