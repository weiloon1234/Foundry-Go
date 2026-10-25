package bootstrap

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberTable binds Member storage and typed relation slots.
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	ID   orm.Key[Member, model.ID[Member]]
	Name orm.Text[Member, string]
}

var Members = orm.Define("bootstrap_members", func(b orm.Builder[Member], m *Member) MemberTable {
	table := orm.TableOf(b, &m.ID)
	return MemberTable{Table: table, ID: table.Key(), Name: orm.TextOf(b, &m.Name)}
})

// OperatorTable binds Operator storage and typed relation slots.
type OperatorTable struct {
	orm.Table[Operator, model.ID[Operator]]
	ID         orm.Key[Operator, model.ID[Operator]]
	Department orm.Text[Operator, string]
}

var Operators = orm.Define("bootstrap_operators", func(b orm.Builder[Operator], m *Operator) OperatorTable {
	table := orm.TableOf(b, &m.ID)
	return OperatorTable{Table: table, ID: table.Key(), Department: orm.TextOf(b, &m.Department)}
})

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Members.Table, Operators.Table} }

var MemberIdentity = orm.IdentityOf(Members.Table, Members.ID)
var OperatorIdentity = orm.IdentityOf(Operators.Table, Operators.ID)
