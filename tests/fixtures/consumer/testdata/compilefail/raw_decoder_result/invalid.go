package invalid

import (
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(d raw.Decoder[string]) raw.Builder[int64] { return raw.NewCommand("GET", d) }
