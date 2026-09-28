package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
)

var disposition string = "attachment"
var bad = h.Stream{}.WithDisposition(disposition)
