package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var header foundryhttp.HeaderName
var _ = foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{header}}
