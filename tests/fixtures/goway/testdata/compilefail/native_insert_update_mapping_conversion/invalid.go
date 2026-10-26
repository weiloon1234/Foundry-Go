package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type scope struct{}

func escape(source orm.InsertMapping[scope, scope]) {
	_ = orm.UpdateMapping[scope, scope](source)
}
