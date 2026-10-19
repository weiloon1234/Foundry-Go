package invalid

import "github.com/weiloon1234/Foundry-Go/database/query"

type first struct{}
type second struct{}

var _ = query.ScalarField[second, string](query.ScalarField[first, string]{})
