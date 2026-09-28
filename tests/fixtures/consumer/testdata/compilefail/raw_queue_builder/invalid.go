package invalid

import (
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(p *raw.Pipeline) { _, _ = raw.Queue(p, raw.NewCommand("GET", raw.DecodeString())) }
