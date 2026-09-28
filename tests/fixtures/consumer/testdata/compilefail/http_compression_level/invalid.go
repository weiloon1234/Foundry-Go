package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var level foundryhttp.BrotliQuality = 4
var _ = foundryhttp.GzipCompression(level)
