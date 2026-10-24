package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type record struct{ ID int64 }

func escape(source orm.Query[record]) {
	_ = orm.Query[record](source.ForUpdate())
}
