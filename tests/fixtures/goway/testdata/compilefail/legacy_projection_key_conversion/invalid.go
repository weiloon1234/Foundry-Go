package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type scope struct{}

func escape(source query.ProjectionKey[scope]) {
	_ = query.Group[scope](source)
}
