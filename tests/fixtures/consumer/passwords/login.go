package passwords

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
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
	return auth.DefineProvider("password.accounts", (Account{}).FoundryReference(), func(ctx context.Context, id model.ID[Account]) (value.Optional[Account], error) {
		return QueryPasswordAccounts().Find(ctx, db, id)
	}, func(_ context.Context, account Account) (bool, error) { return account.Enabled, nil })
}

// NewLogin shares the provider used by session and token guards. MFA policy is
// supplied by the domain; the multifactor fixture demonstrates stored factors.
func NewLogin(db AccountStore, provider AccountProvider, hasher *password.Hasher, requiresMFA func(context.Context, Account) (bool, error)) (*Login, error) {
	return auth.NewPasswordLogin(provider, hasher, auth.PasswordModel[Account, string]{
		Lock: func(ctx context.Context, tx *database.Tx, account Account) (value.Optional[Account], error) {
			return QueryPasswordAccounts().ForUpdate().Find(ctx, tx, account.ID)
		},
		Lookup: func(ctx context.Context, email string) (value.Optional[Account], error) {
			return QueryPasswordAccounts().Where(AccountFields().Email.Eq(email)).First(ctx, db)
		},
		Hash: func(account Account) password.Hash { return account.Digest },
		Rehash: func(ctx context.Context, account Account, old, next password.Hash) (value.Optional[Account], error) {
			updated, err := QueryPasswordAccounts().Where(AccountFields().Digest.Eq(old)).Update(ctx, db, account.ID, AccountDraft{}.SetDigest(next))
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

func Authenticate(ctx context.Context, login *Login, request LoginRequest) (auth.PasswordResult[Account, model.ID[Account]], error) {
	return login.Authenticate(ctx, request.Email, request.Password)
}
