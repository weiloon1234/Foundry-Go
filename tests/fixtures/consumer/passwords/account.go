package passwords

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Account is a model fixture, not a starter application. Digest deliberately
// avoids a conventional sensitive field name to verify type-owned redaction.
type Account struct {
	ID    model.ID[Account]
	Email string
	// Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	Digest password.Hash
	// Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	Backup  value.Nullable[password.Hash]
	Enabled bool
}

type CredentialProjection struct{ Digest password.Hash }

func CreateAccount(ctx context.Context, writer database.Transactor, hasher *password.Hasher, input LoginRequest) (Account, error) {
	hash, err := hasher.Hash(ctx, input.Password)
	if err != nil {
		return Account{}, err
	}
	return AccountRecords.Insert(ctx, writer, AccountRecords.Email.Set(input.Email), AccountRecords.Digest.Set(hash), AccountRecords.Backup.SetNull(), AccountRecords.Enabled.Set(true))
}

// ReplaceHash preserves the stored hash type in both a compare-and-swap predicate
// and its mutation. A concurrent password change cannot be overwritten. Normal
// per-model update hooks still execute; database.NotFound means the CAS lost.
func ReplaceHash(ctx context.Context, writer database.Transactor, account Account, next password.Hash) (Account, error) {
	return orm.Update(ctx, writer, AccountRecords.Query().Where(AccountRecords.Digest.Eq(account.Digest)), AccountRecords.Key(), account.ID, AccountRecords.Digest.Set(next))
}
