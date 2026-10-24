package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.ValueLookup[scope, int64]) { _ = query.UniqueLookup[scope, int64](source) }
