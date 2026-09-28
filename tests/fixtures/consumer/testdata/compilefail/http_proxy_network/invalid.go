package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"net/netip"
)

var address netip.Addr
var _ = foundryhttp.TrustedProxyConfig{Proxies: []netip.Prefix{address}}
