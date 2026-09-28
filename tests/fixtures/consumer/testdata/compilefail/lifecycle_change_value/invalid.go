package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _, _ = lifecycle.CompareField(codec.String[string](), value.Set(1), value.Set("after"), true)
