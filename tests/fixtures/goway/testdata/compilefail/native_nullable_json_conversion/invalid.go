package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type scope struct{}

func escape(source orm.NullableJSON[scope, int64]) {
	_ = orm.JSON[scope, int64](source)
}
