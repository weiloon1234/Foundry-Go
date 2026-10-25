package auditqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// AccountTable declares stored columns and their write roles. Example: Accounts.Key().Eq(key).
type AccountTable struct {
	orm.Table[Account, model.ID[Account]]
	Email        orm.InputText[Account, string, string]
	PasswordHash orm.Text[Account, string]
	Note         orm.NullableText[Account, string]
	Balance      orm.Number[Account, decimal.Decimal]
	Preferences  orm.JSON[Account, Preferences]
	CreatedAt    orm.CreatedAt[Account, temporal.DateTime]
	UpdatedAt    orm.UpdatedAt[Account, temporal.DateTime]
	DeletedAt    orm.SoftDelete[Account, temporal.DateTime]
	Soft         orm.SoftDeletable[Account]
}

// Accounts preserves the persisted audited_accounts model. Example: Accounts.Query().All(ctx,db).
var Accounts = orm.Define("audited_accounts", func(b orm.Builder[Account], m *Account) AccountTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return AccountTable{Table: orm.TableOf(b, &m.ID),
		Email:        orm.InputTextOf[Account, string](b, &m.Email, nil),
		PasswordHash: orm.TextOf(b, &m.PasswordHash),
		Note:         orm.NullableTextOf(b, &m.Note),
		Balance:      orm.NumberOf(b, &m.Balance),
		Preferences:  orm.JSONOf(b, &m.Preferences),
		CreatedAt:    orm.CreatedAtOf(b, &m.CreatedAt),
		UpdatedAt:    orm.UpdatedAtOf(b, &m.UpdatedAt),
		DeletedAt:    deleted,
		Soft:         orm.SoftDeletes(deleted),
	}
}, orm.WithHooks(ormAccountHooks))

// LabelTable declares stored columns and their write roles. Example: Labels.Key().Eq(key).
type LabelTable struct {
	orm.Table[Label, LabelCode]
	Name orm.Text[Label, string]
}

// Labels preserves the persisted audited_labels model. Example: Labels.Query().All(ctx,db).
var Labels = orm.Define("audited_labels", func(b orm.Builder[Label], m *Label) LabelTable {
	return LabelTable{Table: orm.TableOf(b, &m.Code),
		Name: orm.TextOf(b, &m.Name),
	}
})
