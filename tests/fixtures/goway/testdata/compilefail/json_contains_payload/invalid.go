package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() { _ = jsonpaths.Documents.Settings.Contains(value.JSON[[]string]{}) }
