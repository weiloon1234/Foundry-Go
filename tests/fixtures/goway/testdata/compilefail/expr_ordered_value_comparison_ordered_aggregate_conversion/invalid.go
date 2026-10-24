package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type scope struct{}

func escape(source expr.OrderedValueComparison[scope, int64]) {
	_ = expr.OrderedAggregate[scope, int64](source)
}
