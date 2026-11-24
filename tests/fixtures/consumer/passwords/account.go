package passwords

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type CredentialProjection struct{ Digest password.Hash }

func CreateAccount(ctx context.Context, writer database.Transactor, hasher *password.Hasher, input LoginRequest) (Account, error) {
	hash, err := hasher.Hash(ctx, input.Password)
	if err != nil {
		return Account{}, err
	}
	return orm.Insert(ctx, writer, AccountRecords.Table, orm.Set(AccountRecords.Email, input.Email), orm.Set(AccountRecords.Digest, hash), orm.SetNull(AccountRecords.Backup), orm.Set(AccountRecords.Enabled, true))
}

// ReplaceHash preserves the stored hash type in both a compare-and-swap predicate
// and its mutation. A concurrent password change cannot be overwritten. Normal
// per-model update hooks still execute; database.NotFound means the CAS lost.
func ReplaceHash(ctx context.Context, writer database.Transactor, account Account, next password.Hash) (Account, error) {
	return orm.Update(ctx, writer, AccountRecords.Query().Where(orm.Eq(AccountRecords.Digest, account.Digest)), AccountRecords.Key(), account.ID, orm.Set(AccountRecords.Digest, next))
}
