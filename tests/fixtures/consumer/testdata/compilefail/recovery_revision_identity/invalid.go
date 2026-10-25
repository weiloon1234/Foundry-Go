package invalid

import "foundry.test/consumer/recovering"

var member recovering.Member
var _ = recovering.MemberRecords.EmailRevision.Set(member.ID)
