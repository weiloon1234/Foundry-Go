package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var method foundryhttp.Method
var _ = foundryhttp.CORSConfig{Headers: []foundryhttp.HeaderName{method}}
