package invalid

import (
	"github.com/weiloon1234/Foundry-Go/foundation"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var bad = h.AssetsModule("assets", foundation.NewKey[*h.Server]("wrong"), h.DefaultAssetsConfig(h.DirectoryAssets("public")))
