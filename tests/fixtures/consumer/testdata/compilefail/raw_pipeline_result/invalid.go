package invalid

import (
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(r raw.Result[int64]) (string, error) { return r.Value() }
