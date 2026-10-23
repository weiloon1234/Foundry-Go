package sensitive

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Settings struct {
	Region string `json:"region"`
}

//foundry:model table=sensitive_vaults
type Vault struct {
	ID model.ID[Vault]
	// Foundry field behavior (generated): Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.
	Token encrypted.Text
	// Foundry field behavior (generated): Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.
	Backup value.Nullable[encrypted.Text]
	// Foundry field behavior (generated): Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.
	Settings encrypted.JSON[Settings]
	// Foundry field behavior (generated): Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.
	Optional value.Nullable[encrypted.JSON[Settings]]
}

//foundry:model table=sensitive_accounts
type Account struct {
	ID model.ID[Account]
	// Foundry field behavior (generated): Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	Digest password.Hash
	// Foundry field behavior (generated): Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	Backup value.Nullable[password.Hash]
}
