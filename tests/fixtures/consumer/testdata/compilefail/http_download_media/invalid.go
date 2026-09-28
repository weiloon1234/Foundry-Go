package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
)

var wrong = h.Download{}.WithMediaType(h.EntityTag("\"revision\""))
