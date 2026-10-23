package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ model.ID[struct{}]
var _, _ = lifecycle.CompareField(codec.Nullable(codec.String[string]()), value.Set("before"), value.Set(value.Null[string]()), true)
