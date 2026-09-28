package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	"strings"
)

var bad = h.StreamContent{Body: strings.NewReader("missing Close")}
