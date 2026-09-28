package invalid

import (
	"context"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpsigned"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"time"
)

var links httpsigned.Links
var origin foundryhttp.HeaderName = "https://example.test"
var _, _ = links.Asset.URL(context.Background(), origin, httpkernel.AssetPath{}, time.Time{})
