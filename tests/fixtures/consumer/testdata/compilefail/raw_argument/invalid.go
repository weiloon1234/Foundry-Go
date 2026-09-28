package invalid

import (
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(k raw.Key) { _ = raw.NewCommand("SET", raw.DecodeString()).Key(k).Arg("value") }
