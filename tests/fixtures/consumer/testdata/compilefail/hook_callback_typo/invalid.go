package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"
import "foundry.test/consumer/hookqueries"

var _ = orm.Hooks[hookqueries.Member]{Creatng: nil}
