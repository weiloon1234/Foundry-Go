package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.ValueCase[scope, int64]) {
	_ = query.Case[scope, int64](source)
}
