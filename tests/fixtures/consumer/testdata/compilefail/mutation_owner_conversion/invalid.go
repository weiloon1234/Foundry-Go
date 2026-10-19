package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type first struct{}
type second struct{}

var _ = query.Mutation[second](query.Change(query.Assign[first]("records", "id", codec.Signed[int](), 1)))
