package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type scope struct{}

func escape(source orm.UpdateSource[scope, scope]) {
	_ = orm.DeleteSource[scope, scope](source)
}
