package guideadapters_test

import (
	"context"
	"fmt"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin model-references-user-identity
var UserIdentity = orm.IdentityOf(models.Users.Table, models.Users.Key())

func restoreUser(user models.User) (model.Reference[models.User, model.ID[models.User]], error) {
	reference := UserIdentity.ReferenceOf(user)
	identity, err := reference.Identity()
	if err != nil {
		return model.Reference[models.User, model.ID[models.User]]{}, err
	}
	return UserIdentity.Parse(identity)
}

// doc-example:end model-references-user-identity

// doc-example:begin authentication-account
type Account struct {
	ID      int64
	Enabled bool
}

type AccountTable struct {
	orm.Table[Account, int64]
	Enabled orm.Column[Account, bool]
}

var Accounts = orm.Define("guide_accounts", func(b orm.Builder[Account], m *Account) AccountTable {
	return AccountTable{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.Enabled)}
})
var AccountIdentity = orm.IdentityOf(Accounts.Table, Accounts.Key())

func (a Account) FoundryIdentity() (model.Identity, error) {
	return AccountIdentity.ReferenceOf(a).Identity()
}

func accountProvider(db database.Executor) auth.Provider[Account, int64] {
	return auth.DefineProvider("accounts", AccountIdentity.Reference(0),
		func(ctx context.Context, id int64) (value.Optional[Account], error) {
			return orm.Find(ctx, db, Accounts.Table, id)
		},
		func(_ context.Context, account Account) (bool, error) {
			return account.Enabled, nil
		},
	)
}

// doc-example:end authentication-account

// doc-example:begin validation-enabled-accounts
func enabledAccounts(db database.Executor, enabled func(context.Context) (bool, error)) validation.Rule[int64] {
	lookup := databasevalidation.Scoped(db, func(ctx context.Context) (orm.Query[Account], error) {
		value, err := enabled(ctx)
		return Accounts.Query().Where(orm.Eq(Accounts.Enabled, value)), err
	}, Accounts.Key())
	return validation.Exists(lookup)
}

// doc-example:end validation-enabled-accounts

// doc-example:begin developer-tooling-and-testing-users-factory
func usersFactory() (*factory.Factory[models.User, []orm.Assignment[models.User]], error) {
	return factory.Define(models.Users.Table, func(_ context.Context, n factory.Sequence) ([]orm.Assignment[models.User], error) {
		return []orm.Assignment[models.User]{
			orm.Set(models.Users.Email, fmt.Sprintf("reader-%d@example.test", n)),
			orm.Set(models.Users.Age, 18),
			orm.Set(models.Users.Status, models.StatusActive),
			orm.Set(models.Users.Level, models.LevelBasic),
		}, nil
	})
}

// doc-example:end developer-tooling-and-testing-users-factory
