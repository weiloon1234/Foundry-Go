package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var name foundryhttp.HeaderName
var _ = foundryhttp.TrustedProxyConfig{Headers: []foundryhttp.ProxyHeader{name}}
