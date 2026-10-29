package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/softqueries"

var _ = softqueries.MemberLinks.Groups.OnlyTrashedPivot().WherePivot(orm.InputTextEq(softqueries.Members.Name, "wrong model"))
