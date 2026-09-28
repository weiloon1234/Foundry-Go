package invalid

import (
	"github.com/weiloon1234/Foundry-Go/redis"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	pgapp "github.com/weiloon1234/Foundry-Go/testkit/postgres/application"
)

func bad(scope *pgtest.Scope) { _ = pgapp.On(scope, redis.ConnectionName("redis")) }
