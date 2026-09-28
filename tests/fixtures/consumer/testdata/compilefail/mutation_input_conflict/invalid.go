package invalid

import "foundry.test/consumer/inputqueries"

var _ = inputqueries.MemberFields().Email.Set(inputqueries.StoredEmail("already stored"))
