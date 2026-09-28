package invalid

import "foundry.test/consumer/auditqueries"

var _ = auditqueries.AccountAuditing(auditqueries.LabelAuditPolicy{})
