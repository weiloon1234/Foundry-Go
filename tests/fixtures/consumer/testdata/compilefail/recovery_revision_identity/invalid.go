package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/recovering"

var member recovering.Member
var _ = orm.ColumnSet(recovering.MemberRecords.EmailRevision, member.ID)
