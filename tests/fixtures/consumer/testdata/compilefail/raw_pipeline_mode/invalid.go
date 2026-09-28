package invalid

import (
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong() { _, _ = raw.NewPipeline(true) }
