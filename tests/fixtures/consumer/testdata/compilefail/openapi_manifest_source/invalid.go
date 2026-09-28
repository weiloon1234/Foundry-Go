package invalid

import (
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

func invalid() { _, _ = openapi.Render(&manifest.Document{}, openapi.Options{}) }
