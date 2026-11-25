package passwords

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type AccountProvider = auth.Provider[Account, model.ID[Account]]
type Login = auth.PasswordLogin[Account, model.ID[Account], string]

// AccountStore is the consumer's existing pool or transaction, not a second DB.
type AccountStore interface {
	database.Executor
	database.Transactor
}

func Accounts(db database.Executor) AccountProvider {
	return auth.DefineProvider("password.accounts", AccountIdentity.ReferenceOf(Account{}), func(ctx context.Context, id model.ID[Account]) (value.Optional[Account], error) {
		return orm.Find(ctx, db, AccountRecords.Table, id)
	}, func(_ context.Context, account Account) (bool, error) { return account.Enabled, nil })
}

// The adapter uses the same provider as the session and token guards.
// The application supplies its MFA policy.
// doc-example:begin password-login
func passwordLogin(records AccountTable, db interface {
	database.Executor
	database.Transactor
}, provider auth.Provider[Account, model.ID[Account]], hasher *password.Hasher, requiresMFA func(context.Context, Account) (bool, error)) (*auth.PasswordLogin[Account, model.ID[Account], string], error) {
	return auth.NewPasswordLogin(provider, hasher, auth.PasswordModel[Account, string]{
		Lock: func(ctx context.Context, tx *database.Tx, account Account) (value.Optional[Account], error) {
			return orm.FirstLocked(ctx, tx, orm.Lock(records.Query().Where(orm.Eq(records.Key(), account.ID))))
		},
		Lookup: func(ctx context.Context, email string) (value.Optional[Account], error) {
			return orm.First(ctx, db, records.Query().Where(orm.Eq(records.Email, email)))
		},
		Hash: func(account Account) password.Hash { return account.Digest },
		Rehash: func(ctx context.Context, account Account, old, next password.Hash) (value.Optional[Account], error) {
			updated, err := orm.Update(ctx, db, records.Query().Where(orm.Eq(records.Digest, old)), records.Key(), account.ID, orm.Set(records.Digest, next))
			if errors.Is(err, database.NotFound) {
				return value.Optional[Account]{}, nil
			}
			if err != nil {
				return value.Optional[Account]{}, err
			}
			return value.Set(updated), nil
		},
		RequiresMFA: requiresMFA,
	}, auth.DefaultConfig())
}

// doc-example:end password-login

func Authenticate(ctx context.Context, login *Login, request LoginRequest) (auth.PasswordResult[Account, model.ID[Account]], error) {
	return login.Authenticate(ctx, request.Email, request.Password)
}

// NewLogin supplies this application's generated account table to its password adapter.
func NewLogin(db AccountStore, provider AccountProvider, hasher *password.Hasher, requiresMFA func(context.Context, Account) (bool, error)) (*Login, error) {
	return passwordLogin(AccountRecords, db, provider, hasher, requiresMFA)
}
