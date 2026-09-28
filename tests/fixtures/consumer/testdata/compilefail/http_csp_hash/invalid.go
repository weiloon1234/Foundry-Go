package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var wrongDigest [48]byte
var _ = foundryhttp.CSPSHA256(wrongDigest)
