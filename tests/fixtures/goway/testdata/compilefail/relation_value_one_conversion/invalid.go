package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type scope struct{}

func escape(source orm.Value[scope, scope]) {
	_ = orm.One[scope, scope](source)
}
