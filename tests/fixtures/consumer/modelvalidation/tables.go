package modelvalidation

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// UserTable binds the existing consumer model identity to advisory validation.
type UserTable struct {
	orm.Table[models.User, model.ID[models.User]]
	// ID selects the stored user key. Example: Users.ID.Eq(id).
	ID orm.Key[models.User, model.ID[models.User]]
	// Email selects the stored address. Example: Users.Email.Eq(email).
	Email orm.Text[models.User, string]
	// Status selects the stored enum. Example: Users.Status.Eq(models.StatusActive).
	Status orm.Enum[models.User, models.Status]
	// Nickname selects non-null nicknames. Example: Users.Nickname.Eq("member").
	Nickname orm.NullableText[models.User, string]
}

// Users preserves every persisted user column. Example: Users.Query().
var Users = orm.Define("users", func(b orm.Builder[models.User], u *models.User) UserTable {
	t := orm.TableOf(b, &u.ID)
	orm.NumberOf(b, &u.Age)
	orm.EnumOf(b, &u.Level)
	orm.NullableTimeOf(b, &u.Birthday)
	orm.NullableIDOf(b, &u.IntroducerID)
	return UserTable{Table: t, ID: t.Key(), Email: orm.TextOf(b, &u.Email), Status: orm.EnumOf(b, &u.Status), Nickname: orm.NullableTextOf(b, &u.Nickname)}
})

// CountryTable retains the consumer's natural key.
type CountryTable struct {
	orm.Table[models.Country, models.CountryCode]
	// Code selects the stored natural key. Example: Countries.Code.Eq(models.CountryCode("MY")).
	Code orm.Key[models.Country, models.CountryCode]
	// Name selects a stored name. Example: Countries.Name.Eq("Malaysia").
	Name orm.Text[models.Country, string]
}

// Countries binds natural-key validation. Example: Countries.Query().
var Countries = orm.Define("countries", func(b orm.Builder[models.Country], c *models.Country) CountryTable {
	t := orm.TableOf(b, &c.Code)
	return CountryTable{Table: t, Code: t.Key(), Name: orm.TextOf(b, &c.Name)}
})

// LedgerTable retains exact decimal field codecs.
type LedgerTable struct {
	orm.Table[models.LedgerEntry, model.ID[models.LedgerEntry]]
	// Amount selects exact stored decimals. Example: LedgerEntries.Amount.Eq(amount).
	Amount orm.Number[models.LedgerEntry, decimal.Decimal]
}

// LedgerEntries binds decimal validation. Example: LedgerEntries.Query().
var LedgerEntries = orm.Define("ledger_entries", func(b orm.Builder[models.LedgerEntry], e *models.LedgerEntry) LedgerTable {
	t := orm.TableOf(b, &e.ID)
	orm.NullableNumberOf(b, &e.Balance)
	return LedgerTable{Table: t, Amount: orm.NumberOf(b, &e.Amount)}
})

// GroupTable retains the soft-delete visibility of the old source.
type GroupTable struct {
	orm.Table[softqueries.Group, softqueries.GroupCode]
	// Name selects the stored group name. Example: Groups.Name.Eq("reserved").
	Name orm.Text[softqueries.Group, string]
	// DeletedAt controls soft-delete visibility. Delete sets it and Restore clears it.
	// Example: Groups.Query().WithTrashed().
	DeletedAt orm.SoftDelete[softqueries.Group, temporal.DateTime]
}

// Groups binds soft-delete validation. Example: Groups.Query().
var Groups = orm.Define("soft_groups", func(b orm.Builder[softqueries.Group], g *softqueries.Group) GroupTable {
	return GroupTable{Table: orm.TableOf(b, &g.Code), Name: orm.TextOf(b, &g.Name), DeletedAt: orm.SoftDeleteOf(b, &g.DeletedAt)}
})
