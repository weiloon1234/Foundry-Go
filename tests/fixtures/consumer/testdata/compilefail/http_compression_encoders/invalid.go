package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var _ = foundryhttp.CompressionConfig{Encoders: []foundryhttp.HeaderName{"gzip"}}
