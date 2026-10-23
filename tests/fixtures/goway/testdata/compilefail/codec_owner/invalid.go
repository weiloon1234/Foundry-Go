package codec_owner

import (
	m "foundry.test/goway/models"
	codec "github.com/weiloon1234/Foundry-Go/database/codec"
	model "github.com/weiloon1234/Foundry-Go/model"
)

var id model.ID[m.Order]
var _ = codec.ID[m.User]().Scan(&id)
