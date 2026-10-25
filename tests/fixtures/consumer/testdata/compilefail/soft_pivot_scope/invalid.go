package invalid

import "foundry.test/consumer/softqueries"

var _ = softqueries.MemberLinks.Groups.OnlyTrashedPivot().WherePivot(softqueries.Members.Name.Eq("wrong model"))
