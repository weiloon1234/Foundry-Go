package passwords

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// CredentialColumns retains the sensitive result codec. Example: CredentialRows.Digest.
type CredentialColumns struct {
	// Row owns the complete result. Example: rows.Check(CredentialRows).
	rows.Row[CredentialProjection]
	// Digest is a sensitive stored hash, never a cursor key. Example: rows.Map(CredentialRows.Digest,expr.Value(AccountRecords.Digest)).
	Digest rows.Field[CredentialProjection, password.Hash]
}

// CredentialRows binds the handwritten projection. Example: rows.Check(CredentialRows).
var CredentialRows = rows.Define(func(b rows.Builder[CredentialProjection], m *CredentialProjection) CredentialColumns {
	return CredentialColumns{b.Row(), rows.FieldOf(b, &m.Digest, orm.Codec(password.Codec()))}
})
