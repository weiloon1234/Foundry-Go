package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var _ = foundryhttp.TrustedProxyConfig{OriginHeaders: []foundryhttp.ProxyOriginHeader{foundryhttp.ForwardedHeader()}}
