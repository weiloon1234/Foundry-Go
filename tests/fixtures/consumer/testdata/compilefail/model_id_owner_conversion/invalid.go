package invalid

import "github.com/weiloon1234/Foundry-Go/model"

type first struct{}
type second struct{}

var _ = model.ID[second](model.ID[first]{})
