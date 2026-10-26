package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm/expr"

type scope struct{}

func escape(source expr.ProjectionKey[scope]) {
	_ = expr.GroupKey[scope](source)
}
