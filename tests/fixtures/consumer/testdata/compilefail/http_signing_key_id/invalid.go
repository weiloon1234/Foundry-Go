package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var name foundryhttp.CookieName
var _ = foundryhttp.SigningKey{ID: name}
