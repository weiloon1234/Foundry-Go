package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.UpdateSource[scope, scope]) {
	_ = query.DeleteSource[scope, scope](source)
}
