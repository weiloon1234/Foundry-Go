package invalid

import (
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = manifest.Sources{HTTP: foundryhttp.StaticPath("/")}
