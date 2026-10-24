package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.ValueComparison[scope, int64]) {
	_ = query.RowExpression[scope, int64](source)
}
