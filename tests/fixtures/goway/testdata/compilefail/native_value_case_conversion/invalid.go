package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm/expr"

type scope struct{}

func escape(source expr.ValueCase[scope, int64]) {
	_ = expr.Case[scope, int64](source)
}
