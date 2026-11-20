package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var _ = modelbinding.ByColumn(nil, sensitive.Vaults.Query(), sensitive.Vaults.Token, func(encrypted.Text) encrypted.Text { return encrypted.NewText("x") })
