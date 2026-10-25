package multifactor

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Account struct {
	ID    model.ID[Account]
	Email string
	// Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	Password   password.Hash
	Enabled    bool
	MFAEnabled bool
	RequireMFA bool
}

type Provider = auth.Provider[Account, model.ID[Account]]
type Login = auth.PasswordLogin[Account, model.ID[Account], string]
type PasswordResult = auth.PasswordResult[Account, model.ID[Account]]
type Factors = mfa.Factors[Account, model.ID[Account]]

// FactorAttempts is distinct from the password family's submitted-email key.
// Shared Redis deployments use a namespace appropriate to the model's tenant.
var FactorAttempts = lockout.Define("mfa.accounts", keyspace.TextKeys[model.ID[Account]](), lockout.DefaultPolicy())

func Accounts(db database.Executor) Provider {
	return auth.DefineProvider("mfa.accounts", AccountIdentity.ReferenceOf(Account{}), func(ctx context.Context, id model.ID[Account]) (value.Optional[Account], error) {
		return AccountRecords.Find(ctx, db, id)
	}, func(_ context.Context, account Account) (bool, error) { return account.Enabled, nil })
}
func NewLogin(db interface {
	database.Executor
	database.Transactor
}, provider Provider, hasher *password.Hasher) (*Login, error) {
	return auth.NewPasswordLogin(provider, hasher, auth.PasswordModel[Account, string]{
		Lookup: func(ctx context.Context, email string) (value.Optional[Account], error) {
			return AccountRecords.Query().Where(AccountRecords.Email.Eq(email)).First(ctx, db)
		},
		Lock: func(ctx context.Context, tx *database.Tx, prior Account) (value.Optional[Account], error) {
			return lockAccount(ctx, tx, prior.ID)
		},
		Hash: func(account Account) password.Hash { return account.Password },
		Rehash: func(ctx context.Context, account Account, old, next password.Hash) (value.Optional[Account], error) {
			updated, err := orm.Update(ctx, db, AccountRecords.Query().Where(AccountRecords.Password.Eq(old)), AccountRecords.Key(), account.ID, AccountRecords.Password.Set(next))
			if errors.Is(err, database.NotFound) {
				return value.Optional[Account]{}, nil
			}
			if err != nil {
				return value.Optional[Account]{}, err
			}
			return value.Set(updated), nil
		},
		RequiresMFA: func(_ context.Context, account Account) (bool, error) {
			return account.MFAEnabled || account.RequireMFA, nil
		},
	}, auth.DefaultConfig())
}
func NewFactors(store *mfa.Store, provider Provider, attempts lockout.Throttle[model.ID[Account]], invalidate func(context.Context, *database.Tx, Account) error) (*Factors, error) {
	return mfa.New(store, provider, FactorModel(invalidate), attempts)
}

// FactorModel centralizes stored-field mapping for management and completion.
func FactorModel(invalidate func(context.Context, *database.Tx, Account) error) mfa.Model[Account, model.ID[Account]] {
	return mfa.Model[Account, model.ID[Account]]{
		Lock:    lockAccount,
		Enabled: func(account Account) bool { return account.MFAEnabled },
		SetEnabled: func(ctx context.Context, tx *database.Tx, account Account, enabled bool) (Account, error) {
			return AccountRecords.Update(ctx, tx, account.ID, AccountRecords.MFAEnabled.Set(enabled))
		},
		AccountLabel: func(account Account) (string, error) { return account.Email, nil },
		CanDisable:   func(_ context.Context, account Account) (bool, error) { return !account.RequireMFA, nil },
		Invalidate:   invalidate,
	}
}

func BeginEnrollment(ctx context.Context, factors *Factors, password PasswordResult) (mfa.Enrollment[Account], error) {
	return factors.Enroll(ctx, password)
}
func ConfirmEnrollment(ctx context.Context, factors *Factors, password PasswordResult, id mfa.EnrollmentID[Account], request TOTPRequest) (mfa.RecoveryCodes[Account], error) {
	return factors.Confirm(ctx, password, id, request.Code)
}
func RotateRecoveryCodes(ctx context.Context, factors *Factors, password PasswordResult, request RecoveryRequest) (mfa.RecoveryCodes[Account], error) {
	response, err := mfa.RecoveryResponse(request.Code)
	if err != nil {
		return mfa.RecoveryCodes[Account]{}, err
	}
	return factors.RegenerateRecovery(ctx, password, response)
}

func lockAccount(ctx context.Context, tx *database.Tx, id model.ID[Account]) (value.Optional[Account], error) {
	return AccountRecords.Query().Where(AccountRecords.Key().Eq(id)).ForUpdate().First(ctx, tx)
}
