package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type scope struct{}

func escape(source orm.JSON[scope, int64]) {
	_ = orm.Number[scope, int64](source)
}
