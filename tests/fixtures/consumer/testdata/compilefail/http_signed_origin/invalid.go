package invalid

import (
	"context"
	"time"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpsigned"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var links httpsigned.Links
var origin foundryhttp.HeaderName = "https://example.test"
var _, _ = foundryhttp.SignedRouteURL(links.Asset, context.Background(), origin, httpkernel.AssetPath{}, time.Time{})
