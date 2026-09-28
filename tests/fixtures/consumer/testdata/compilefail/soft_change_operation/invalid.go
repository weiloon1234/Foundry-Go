package invalid

import "foundry.test/consumer/softqueries"

var _ string = softqueries.MemberChanges{}.Operation()
