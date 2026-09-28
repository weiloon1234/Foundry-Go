package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
)

var media string = "text/plain"
var bad = h.StreamResponse(media)
