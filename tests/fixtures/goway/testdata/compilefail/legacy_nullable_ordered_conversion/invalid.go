package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.NullableField[scope, int64]) {
	_ = query.OrderedField[scope, int64](source)
}
