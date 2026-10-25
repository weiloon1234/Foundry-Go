package invalid

import (
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type account struct {
	ID    int
	Email string
}
type label struct{ ID int }
type fields struct {
	orm.Table[account, int]
	Email orm.Text[account, string]
}

var accounts = orm.Define("accounts", func(b orm.Builder[account], m *account) fields {
	return fields{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Email)}
})

func invalid(history record.Model[account, int]) {
	field, _ := orm.AuditRead(history, accounts.Email)
	var _ value.Optional[record.FieldChange[int]] = field
}
