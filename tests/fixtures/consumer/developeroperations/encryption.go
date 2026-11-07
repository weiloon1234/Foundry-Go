package developeroperations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// doc-example:begin load-record-keys
func LoadRecordKeys(id encryption.KeyID, material secret.String, previous ...encryption.Key) (*encryption.Keyring, error) {
	active, err := encryption.ParseKey(id, material)
	if err != nil {
		return nil, err
	}
	return encryption.NewKeyring(id, append([]encryption.Key{active}, previous...)...)
}

// doc-example:end load-record-keys

// doc-example:begin encrypt-record
func RecordContext(trustedRecordBinding secret.String) (encryption.Context, error) {
	return encryption.NewContext("consumer.records.secret.v1", trustedRecordBinding)
}

func EncryptRecord(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, plaintext secret.String) (encryption.Ciphertext, error) {
	return keys.Encrypt(ctx, owner, plaintext)
}

func DecryptRecord(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, ciphertext encryption.Ciphertext) (secret.String, error) {
	return keys.Decrypt(ctx, owner, ciphertext)
}

func ReencryptRecord(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, ciphertext encryption.Ciphertext) (encryption.Ciphertext, error) {
	return keys.Reencrypt(ctx, owner, ciphertext)
}

// doc-example:end encrypt-record

// doc-example:begin configured-encryption
func ConfiguredKeys(services application.Services) (*encryption.Keyring, error) {
	return services.Encryption()
}

// doc-example:end configured-encryption
