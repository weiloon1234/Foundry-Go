package invalid

import "foundry.test/consumer/inputqueries"

var _ = inputqueries.MemberFields().Email.Eq(inputqueries.EmailInput{})
