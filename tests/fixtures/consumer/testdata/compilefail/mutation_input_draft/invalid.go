package invalid

import "foundry.test/consumer/inputqueries"

var _ = inputqueries.MemberDraft{}.SetEmail(inputqueries.StoredEmail("already stored"))
