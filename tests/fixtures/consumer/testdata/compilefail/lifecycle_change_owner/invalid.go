package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Member struct{}
type Company struct{}

var _, _ = lifecycle.CompareField(codec.ID[Member](), value.Set(model.ID[Company]{}), value.Set(model.ID[Member]{}), true)
