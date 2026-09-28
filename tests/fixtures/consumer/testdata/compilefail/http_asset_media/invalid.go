package invalid

import h "github.com/weiloon1234/Foundry-Go/http"

var bad = h.AssetsConfig{Media: map[h.AssetExtension]h.MediaType{".css": 12}}
