package vault

import (
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// EntryTable exposes plaintext setters and preserves row-bound encryption.
// Example: Entries.Token.Set(encrypted.NewText("secret")).
type EntryTable struct {
	orm.Table[Entry, model.ID[Entry]]
	Label    orm.Text[Entry, string]
	Token    orm.Encrypted[Entry, encrypted.Text]
	Settings orm.NullableEncrypted[Entry, encrypted.JSON[Settings]]
}

// Entries owns encrypted hydration and persistence. Example: Entries.Require(ctx,db,id).
var Entries = orm.Define("vault_entries", func(b orm.Builder[Entry], m *Entry) EntryTable {
	return EntryTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Label), orm.EncryptedTextOf(b, &m.Token), orm.NullableEncryptedJSONOf(b, &m.Settings)}
})
