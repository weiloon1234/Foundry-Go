package guideadapters_test

import (
	"context"
	"fmt"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// guide:identity
// doc-example:begin model-references-user-identity
var UserIdentity = orm.IdentityOf(models.Users.Table, models.Users.Key())

func restoreUser(user models.User) (model.Reference[models.User, model.ID[models.User]], error) {
	reference := UserIdentity.ReferenceOf(user)
	identity, err := reference.Identity()
	if err != nil {
		return model.Reference[models.User, model.ID[models.User]]{}, err
	}
	return UserIdentity.Parse(identity)
} // doc-example:end model-references-user-identity

// guide:end

// guide:auth-model
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
} // doc-example:end authentication-account

// guide:end

// guide:attribution
// doc-example:begin model-references-with-origin
func withOrigin(ctx context.Context, account Account) (context.Context, error) {
	origin, err := (attribution.Origin{}).WithModel(account)
	if err != nil {
		return nil, err
	}
	origin, err = origin.WithGuard("session")
	if err != nil {
		return nil, err
	}
	return attribution.WithContext(ctx, origin)
} // doc-example:end model-references-with-origin

// guide:end

// guide:pages
// doc-example:begin model-pagination-numbered
func numbered(ctx context.Context, db database.Executor) (orm.PageResult[models.User], error) {
	return orm.Paginate(ctx, db, models.Users.Query().Where(orm.NumberGte(models.Users.Age, 18)), orm.Page(1, 20))
}

func simple(ctx context.Context, db database.Executor) (orm.SimplePage[models.User], error) {
	return orm.SimplePaginate(ctx, db, models.Users.Query().Where(orm.NumberGte(models.Users.Age, 18)), orm.Page(1, 20))
} // doc-example:end model-pagination-numbered

// guide:end

// guide:cursors
// doc-example:begin model-pagination-cursor-page
func cursorPage(ctx context.Context, db database.Executor, request orm.CursorRequest[models.User]) (orm.CursorPage[models.User], error) {
	q := models.Users.Query().OrderBy(orm.TextAsc(models.Users.Email))
	return orm.CursorPaginate(ctx, db, q, request)
} // doc-example:end model-pagination-cursor-page

// guide:end

// guide:validation
// doc-example:begin database-rules
func availableEmail(db database.Executor) validation.Rule[string] {
	return databasevalidation.Unique(db, models.Users.Query(), orm.TextExpr(models.Users.Email))
}

func activeUsers(db database.Executor) validation.Rule[[]model.ID[models.User]] {
	return databasevalidation.ExistsAll(db,
		models.Users.Query().Where(orm.EnumEq(models.Users.Status, models.StatusActive)),
		orm.KeyExpr(models.Users.Key()))
} // doc-example:end database-rules

// guide:end

// guide:validation-ignore
// doc-example:begin validation-email-change
type EmailChange struct {
	ID    model.ID[models.User]
	Email string
}

func updateRules(db database.Executor) validation.Rule[EmailChange] {
	current := validation.NewSlot[model.ID[models.User]]()
	email := databasevalidation.UniqueIgnoring(db, models.Users.Query(), orm.TextExpr(models.Users.Email), orm.KeyExpr(models.Users.Key()), current)
	field := validation.DefineField("email", func(in EmailChange) string { return in.Email })
	return validation.Provide(current,
		func(_ context.Context, in EmailChange) (model.ID[models.User], error) { return in.ID, nil },
		validation.Rules(field, email))
} // doc-example:end validation-email-change

// guide:end

// guide:validation-scope
// doc-example:begin validation-enabled-accounts
func enabledAccounts(db database.Executor, enabled func(context.Context) (bool, error)) validation.Rule[int64] {
	lookup := databasevalidation.Scoped(db, func(ctx context.Context) (orm.Query[Account], error) {
		value, err := enabled(ctx)
		return Accounts.Query().Where(orm.ColumnEq(Accounts.Enabled, value)), err
	}, orm.KeyExpr(Accounts.Key()))
	return validation.Exists(lookup)
} // doc-example:end validation-enabled-accounts

// guide:end

// guide:validation-each
// doc-example:begin validation-expanded-assignment
type Assignment struct{ Assignee model.ID[models.User] }

func assignees(db database.Executor) validation.Rule[[]Assignment] {
	field := validation.DefineField("assignee_id", func(a Assignment) model.ID[models.User] { return a.Assignee })
	return databasevalidation.ExistsEach(db,
		models.Users.Query().Where(orm.EnumEq(models.Users.Status, models.StatusActive)),
		orm.KeyExpr(models.Users.Key()), field)
} // doc-example:end validation-expanded-assignment

// guide:end

// guide:factory
// doc-example:begin developer-tooling-and-testing-users-factory
func usersFactory() (*factory.Factory[models.User, []orm.Assignment[models.User]], error) {
	return factory.Define(models.Users.Table, func(_ context.Context, n factory.Sequence) ([]orm.Assignment[models.User], error) {
		return []orm.Assignment[models.User]{
			orm.TextSet(models.Users.Email, fmt.Sprintf("reader-%d@example.test", n)),
			orm.NumberSet(models.Users.Age, 18),
			orm.EnumSet(models.Users.Status, models.StatusActive),
			orm.EnumSet(models.Users.Level, models.LevelBasic),
		}, nil
	})
} // doc-example:end developer-tooling-and-testing-users-factory

// guide:end
