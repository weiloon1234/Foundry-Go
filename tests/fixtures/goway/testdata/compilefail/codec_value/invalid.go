package codec_value

import (
	c "foundry.test/goway/coremodels"
	codec "github.com/weiloon1234/Foundry-Go/database/codec"
)

var _, _ = codec.String[c.Code]().Bind(42)
