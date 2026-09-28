package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var referrer foundryhttp.ReferrerPolicy
var _ = foundryhttp.SecurityHeadersConfig{Frame: referrer}
