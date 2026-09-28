package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type First string
type Second string

var _ foundryhttp.PathCodec[First] = foundryhttp.URLType("app.Second", foundryhttp.StringPath[Second]())
