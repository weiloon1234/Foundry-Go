package invalid

import "foundry.test/consumer/softqueries"

var _ = softqueries.MemberRelations().Groups.OnlyTrashedPivot().WherePivot(softqueries.MemberFields().Name.Eq("wrong model"))
