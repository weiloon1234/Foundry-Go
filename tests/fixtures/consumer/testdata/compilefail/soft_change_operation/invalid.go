package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"
import "foundry.test/consumer/softqueries"

var _ string = orm.Changes[softqueries.Member]{}.Operation()
