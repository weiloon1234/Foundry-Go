// Package tokenstore owns PostgreSQL token families and retained generations.
// Applications consume auth/token's typed interfaces, never these storage rows.
package tokenstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Subject is a stable lock identity for create/revoke/refresh serialization.
type Subject struct {
	Key      string
	Scope    string
	Identity value.JSON[model.Identity]
}

// Family owns identity, grant, immutable expiry policy and the current generation.
type Family struct {
	ID               model.ID[Family]
	Scope            string
	SubjectKey       string
	Name             string
	Scopes           value.JSON[[]auth.AccessScopeName]
	Mode             uint8
	Assurance        uint8
	AccessNanos      int64
	RefreshIdleNanos int64
	RotationLimit    uint32
	Generation       uint32
	CreatedAt        temporal.DateTime
	ExpiresAt        temporal.DateTime
	// ClientIP and UserAgent are display-only device metadata from issuance.
	ClientIP  value.Nullable[string]
	UserAgent value.Nullable[string]
}

// Entry retains the current generation and the one before it. The previous
// access hash authenticates only within the configured refresh grace; its
// refresh hash, like every older one kept in foundry_token_consumed_refreshes,
// identifies reuse and revokes the family.
type Entry struct {
	ID               model.ID[Entry]
	Scope            string
	FamilyID         model.ID[Family]
	Generation       uint32
	AccessHash       string
	RefreshHash      value.Nullable[string]
	IssuedAt         temporal.DateTime
	LastSeenAt       temporal.DateTime
	AccessExpiresAt  temporal.DateTime
	RefreshExpiresAt value.Nullable[temporal.DateTime]
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("token subject")) }
func (Family) Format(s fmt.State, _ rune)  { _, _ = s.Write([]byte("stored token family")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored token generation")) }

// SubjectTable binds the persisted Subject columns. Example: Subjects.Query().Limit(10).
type SubjectTable struct {
	// Table provides Subject queries and writes. Example: Subjects.Query().Limit(10).
	orm.Table[Subject, string]
	// Scope binds the persisted Subject.Scope column.
	// Example: Subjects.Scope.Set(scope).
	Scope orm.Text[Subject, string]
	// Identity binds the persisted Subject.Identity column.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = Subjects.Identity.Set(encoded) }.
	Identity orm.JSON[Subject, model.Identity]
}

// Subjects owns the immutable Subject declaration. Example: Subjects.Query().First(ctx, tx).
var Subjects = orm.Define("foundry_token_subjects", func(b orm.Builder[Subject], m *Subject) SubjectTable {
	table := orm.TableOf(b, &m.Key)
	return SubjectTable{Table: table,
		Scope:    orm.TextOf(b, &m.Scope),
		Identity: orm.JSONOf(b, &m.Identity),
	}
})

// FamilyTable binds the persisted Family columns. Example: Families.Query().Limit(10).
type FamilyTable struct {
	// Table provides Family queries and writes. Example: Families.Query().Limit(10).
	orm.Table[Family, model.ID[Family]]
	// ID binds the persisted Family.ID column.
	// Example: Families.ID.Eq(id).
	ID orm.Key[Family, model.ID[Family]]
	// Scope binds the persisted Family.Scope column.
	// Example: Families.Scope.Set(scope).
	Scope orm.Text[Family, string]
	// SubjectKey binds the persisted Family.SubjectKey column.
	// Example: Families.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[Family, string]
	// Name binds the persisted Family.Name column.
	// Example: Families.Name.Set(name).
	Name orm.Text[Family, string]
	// Scopes binds the persisted Family.Scopes column.
	// Example: encoded, err := value.NewJSON(scopes); if err == nil { _ = Families.Scopes.Set(encoded) }.
	Scopes orm.JSON[Family, []auth.AccessScopeName]
	// Mode binds the persisted Family.Mode column.
	// Example: Families.Mode.Set(mode).
	Mode orm.Number[Family, uint8]
	// Assurance binds the persisted Family.Assurance column.
	// Example: Families.Assurance.Set(assurance).
	Assurance orm.Number[Family, uint8]
	// AccessNanos binds the persisted Family.AccessNanos column.
	// Example: Families.AccessNanos.Set(accessNanos).
	AccessNanos orm.Number[Family, int64]
	// RefreshIdleNanos binds the persisted Family.RefreshIdleNanos column.
	// Example: Families.RefreshIdleNanos.Set(refreshIdleNanos).
	RefreshIdleNanos orm.Number[Family, int64]
	// RotationLimit binds the persisted Family.RotationLimit column.
	// Example: Families.RotationLimit.Set(rotationLimit).
	RotationLimit orm.Number[Family, uint32]
	// Generation binds the persisted Family.Generation column.
	// Example: Families.Generation.Set(generation).
	Generation orm.Number[Family, uint32]
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: Families.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Family, temporal.DateTime]
	// ExpiresAt binds the persisted Family.ExpiresAt column.
	// Example: Families.ExpiresAt.Set(expiresAt).
	ExpiresAt orm.Time[Family, temporal.DateTime]
	// ClientIP binds the persisted Family.ClientIP column.
	// Example: Families.ClientIP.SetNull().
	ClientIP orm.NullableText[Family, string]
	// UserAgent binds the persisted Family.UserAgent column.
	// Example: Families.UserAgent.SetNull().
	UserAgent orm.NullableText[Family, string]
}

// Families owns the immutable Family declaration. Example: Families.Query().First(ctx, tx).
var Families = orm.Define("foundry_token_families", func(b orm.Builder[Family], m *Family) FamilyTable {
	table := orm.TableOf(b, &m.ID)
	return FamilyTable{Table: table,
		ID:               table.Key(),
		Scope:            orm.TextOf(b, &m.Scope),
		SubjectKey:       orm.TextOf(b, &m.SubjectKey),
		Name:             orm.TextOf(b, &m.Name),
		Scopes:           orm.JSONOf(b, &m.Scopes),
		Mode:             orm.NumberOf(b, &m.Mode),
		Assurance:        orm.NumberOf(b, &m.Assurance),
		AccessNanos:      orm.NumberOf(b, &m.AccessNanos),
		RefreshIdleNanos: orm.NumberOf(b, &m.RefreshIdleNanos),
		RotationLimit:    orm.NumberOf(b, &m.RotationLimit),
		Generation:       orm.NumberOf(b, &m.Generation),
		CreatedAt:        orm.TimeOf(b, &m.CreatedAt),
		ExpiresAt:        orm.TimeOf(b, &m.ExpiresAt),
		ClientIP:         orm.NullableTextOf(b, &m.ClientIP),
		UserAgent:        orm.NullableTextOf(b, &m.UserAgent),
	}
})

// EntryTable binds the persisted Entry columns. Example: Entries.Query().Limit(10).
type EntryTable struct {
	// Table provides Entry queries and writes. Example: Entries.Query().Limit(10).
	orm.Table[Entry, model.ID[Entry]]
	// ID binds the persisted Entry.ID column.
	// Example: Entries.ID.Eq(id).
	ID orm.Key[Entry, model.ID[Entry]]
	// Scope binds the persisted Entry.Scope column.
	// Example: Entries.Scope.Set(scope).
	Scope orm.Text[Entry, string]
	// FamilyID binds the persisted Entry.FamilyID column.
	// Example: Entries.FamilyID.Set(familyID).
	FamilyID orm.Column[Entry, model.ID[Family]]
	// Generation binds the persisted Entry.Generation column.
	// Example: Entries.Generation.Set(generation).
	Generation orm.Number[Entry, uint32]
	// AccessHash binds the persisted Entry.AccessHash column.
	// Example: Entries.AccessHash.Set(accessHash).
	AccessHash orm.Text[Entry, string]
	// RefreshHash binds the persisted Entry.RefreshHash column.
	// Example: Entries.RefreshHash.SetNull().
	RefreshHash orm.NullableText[Entry, string]
	// IssuedAt binds the persisted Entry.IssuedAt column.
	// Example: Entries.IssuedAt.Set(issuedAt).
	IssuedAt orm.Time[Entry, temporal.DateTime]
	// LastSeenAt binds the persisted Entry.LastSeenAt column.
	// Example: Entries.LastSeenAt.Set(lastSeenAt).
	LastSeenAt orm.Time[Entry, temporal.DateTime]
	// AccessExpiresAt binds the persisted Entry.AccessExpiresAt column.
	// Example: Entries.AccessExpiresAt.Set(accessExpiresAt).
	AccessExpiresAt orm.Time[Entry, temporal.DateTime]
	// RefreshExpiresAt binds the persisted Entry.RefreshExpiresAt column.
	// Example: Entries.RefreshExpiresAt.SetNull().
	RefreshExpiresAt orm.NullableTime[Entry, temporal.DateTime]
}

// Entries owns the immutable Entry declaration. Example: Entries.Query().First(ctx, tx).
var Entries = orm.Define("foundry_token_generations", func(b orm.Builder[Entry], m *Entry) EntryTable {
	table := orm.TableOf(b, &m.ID)
	return EntryTable{Table: table,
		ID:               table.Key(),
		Scope:            orm.TextOf(b, &m.Scope),
		FamilyID:         orm.ColumnOf(b, &m.FamilyID),
		Generation:       orm.NumberOf(b, &m.Generation),
		AccessHash:       orm.TextOf(b, &m.AccessHash),
		RefreshHash:      orm.NullableTextOf(b, &m.RefreshHash),
		IssuedAt:         orm.TimeOf(b, &m.IssuedAt),
		LastSeenAt:       orm.TimeOf(b, &m.LastSeenAt),
		AccessExpiresAt:  orm.TimeOf(b, &m.AccessExpiresAt),
		RefreshExpiresAt: orm.NullableTimeOf(b, &m.RefreshExpiresAt),
	}
})
