package auditprobe

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Declare() record.Declaration {
	return orm.Auditing(h.Members.Table, orm.AuditPolicyOf(h.Members.Table, orm.AuditField(h.Members.Body, record.Exclude)))
}
func Capture(c orm.Changes[h.Member]) (record.Model[h.Member, int64], error) {
	return orm.Audit(h.Members.Table, c, orm.AuditPolicy[h.Member]{})
}
func Read(his record.Model[h.Member, int64]) error {
	_, err := orm.AuditReadNullable(his, h.Members.Note)
	return err
}
