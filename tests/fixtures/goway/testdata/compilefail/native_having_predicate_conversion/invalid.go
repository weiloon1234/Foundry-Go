package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm/expr"

type scope struct{}

func escape(source expr.HavingPredicate[scope]) {
	_ = expr.Predicate[scope](source)
}
