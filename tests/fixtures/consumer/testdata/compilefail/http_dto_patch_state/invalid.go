package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = httpdto.UpdateUser{Nickname: value.Set("missing nullable wrapper")}
