package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
)

var invalid, _ = codec.String[models.CountryCode]().Bind(42)
