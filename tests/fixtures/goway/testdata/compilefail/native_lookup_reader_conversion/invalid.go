package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type scope struct{}

func escape(source orm.ValueLookup[scope, int64]) {
	_ = orm.KeyLookup[scope, int64](source)
}
