package invalid

import (
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
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

func invalid(history record.Model[label, int]) { _, _ = orm.AuditRead(history, accounts.Email) }
