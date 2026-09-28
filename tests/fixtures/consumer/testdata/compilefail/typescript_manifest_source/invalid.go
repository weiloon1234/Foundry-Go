package invalid

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func invalid() { _, _ = typescript.Render(contract.StringJSON[string]()) }
