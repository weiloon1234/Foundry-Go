package invalid

import (
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.NewObserver[observerqueries.Record]("wrong").Declare(func() orm.Hooks[observerqueries.Plain] { return orm.Hooks[observerqueries.Plain]{} })
