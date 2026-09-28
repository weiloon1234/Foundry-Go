package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

var bad = h.StreamContent{Length: value.Set("10")}
