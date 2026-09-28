package invalid

import "foundry.test/consumer/recovering"

var member recovering.Member
var _ = recovering.MemberDraft{}.SetEmailRevision(member.ID)
