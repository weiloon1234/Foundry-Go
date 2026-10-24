package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.InsertMapping[scope, scope]) { _ = query.UpdateMapping[scope, scope](source) }
