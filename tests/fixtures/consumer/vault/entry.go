// Package vault demonstrates encrypted model fields: a typed model stores
// plaintext-facing encrypted.Text and encrypted.JSON values as bound envelopes.
package vault

import (
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Entry keeps an API token and optional connection settings encrypted at rest.
//
//foundry:model table=vault_entries
type Entry struct {
	ID    model.ID[Entry]
	Label string
	// Foundry field behavior (generated): Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.
	Token encrypted.Text
	// Foundry field behavior (generated): Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.
	Settings value.Nullable[encrypted.JSON[Settings]]
}

// Settings is ordinary typed JSON before encryption.
type Settings struct {
	Region string `json:"region"`
	Limits []int  `json:"limits"`
}

const (
	Origin        migrate.Origin  = "consumer.vault"
	CreateEntries migrate.ID      = "202610010001_vault_entries"
	Introduced    migrate.Version = "v0.1.0"
)

// Migrations stores envelopes as text; the column never holds plaintext.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: Origin, ID: CreateEntries}, Version: Introduced, SQL: []string{
		`CREATE TABLE vault_entries (id uuid PRIMARY KEY, label text NOT NULL, token text NOT NULL, settings text NULL)`,
	}}}
}
