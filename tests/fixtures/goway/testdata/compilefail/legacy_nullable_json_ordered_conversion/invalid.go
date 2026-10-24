package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

type scope struct{}

func escape(source query.NullableJSONField[scope, value.JSON[scope]]) {
	_ = query.NullableOrderedField[scope, value.JSON[scope]](source)
}
