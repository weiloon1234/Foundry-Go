package invalid

import (
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

var wrong ratelimit.Declaration[string] = ratelimit.Define("requests", keyspace.SignedKeys[int64](), ratelimit.PerSecond(1))
