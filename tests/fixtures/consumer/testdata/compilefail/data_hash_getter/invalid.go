package invalid

import (
	"foundry.test/consumer/redisdata"
)

func wrong(raw string) redisdata.Profile { return redisdata.Profile{Email: raw} }
