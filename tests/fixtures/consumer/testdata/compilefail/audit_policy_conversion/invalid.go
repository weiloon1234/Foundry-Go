package invalid

import "foundry.test/consumer/observerqueries"

var _ = observerqueries.PlainAuditPolicy(observerqueries.RecordAuditPolicy{})
