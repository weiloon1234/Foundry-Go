package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type first struct{}
type second struct{}

var _ = query.Assignment[second](query.Assign[first]("records", "id", codec.Signed[int](), 1))
