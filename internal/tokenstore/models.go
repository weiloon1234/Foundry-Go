// Package tokenstore owns PostgreSQL token families and retained generations.
// Applications consume auth/token's typed interfaces, never these storage rows.
package tokenstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Subject is a stable lock identity for create/revoke/refresh serialization.
//
//foundry:model table=foundry_token_subjects var=Subjects key=Key identity=false
type Subject struct {
	// Table provides Subject queries and writes. Example: Subjects.Query().Limit(10).
	Key string
	// Scope binds the persisted Subject.Scope column.
	// Example: orm.Set(Subjects.Scope, scope).
	Scope string
	// Identity binds the persisted Subject.Identity column.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Subjects.Identity, encoded) }.
	Identity value.JSON[model.Identity]
}

// Family owns identity, grant, immutable expiry policy and the current generation.
//
//foundry:model table=foundry_token_families var=Families key=ID identity=false
type Family struct {
	// Table provides Family queries and writes. Example: Families.Query().Limit(10).
	//
	// ID binds the persisted Family.ID column.
	// Example: orm.Eq(Families.ID, id).
	ID model.ID[Family] `foundry:"member=ID"`
	// Scope binds the persisted Family.Scope column.
	// Example: orm.Set(Families.Scope, scope).
	Scope string
	// SubjectKey binds the persisted Family.SubjectKey column.
	// Example: orm.Set(Families.SubjectKey, subjectKey).
	SubjectKey string
	// Name binds the persisted Family.Name column.
	// Example: orm.Set(Families.Name, name).
	Name string
	// Scopes binds the persisted Family.Scopes column.
	// Example: encoded, err := value.NewJSON(scopes); if err == nil { _ = orm.Set(Families.Scopes, encoded) }.
	Scopes value.JSON[[]auth.AccessScopeName]
	// Mode binds the persisted Family.Mode column.
	// Example: orm.Set(Families.Mode, mode).
	Mode uint8
	// Assurance binds the persisted Family.Assurance column.
	// Example: orm.Set(Families.Assurance, assurance).
	Assurance uint8
	// AccessNanos binds the persisted Family.AccessNanos column.
	// Example: orm.Set(Families.AccessNanos, accessNanos).
	AccessNanos int64
	// RefreshIdleNanos binds the persisted Family.RefreshIdleNanos column.
	// Example: orm.Set(Families.RefreshIdleNanos, refreshIdleNanos).
	RefreshIdleNanos int64
	// RotationLimit binds the persisted Family.RotationLimit column.
	// Example: orm.Set(Families.RotationLimit, rotationLimit).
	RotationLimit uint32
	// Generation binds the persisted Family.Generation column.
	// Example: orm.Set(Families.Generation, generation).
	Generation uint32
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: orm.Set(Families.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
	// ExpiresAt binds the persisted Family.ExpiresAt column.
	// Example: orm.Set(Families.ExpiresAt, expiresAt).
	ExpiresAt temporal.DateTime
	// ClientIP and UserAgent are display-only device metadata from issuance.
	//
	// ClientIP binds the persisted Family.ClientIP column.
	// Example: orm.SetNull(Families.ClientIP).
	ClientIP value.Nullable[string]
	// UserAgent binds the persisted Family.UserAgent column.
	// Example: orm.SetNull(Families.UserAgent).
	UserAgent value.Nullable[string]
}

// Entry retains the current generation and the one before it. The previous
// access hash authenticates only within the configured refresh grace; its
// refresh hash, like every older one kept in foundry_token_consumed_refreshes,
// identifies reuse and revokes the family.
//
//foundry:model table=foundry_token_generations var=Entries key=ID identity=false
type Entry struct {
	// Table provides Entry queries and writes. Example: Entries.Query().Limit(10).
	//
	// ID binds the persisted Entry.ID column.
	// Example: orm.Eq(Entries.ID, id).
	ID model.ID[Entry] `foundry:"member=ID"`
	// Scope binds the persisted Entry.Scope column.
	// Example: orm.Set(Entries.Scope, scope).
	Scope string
	// FamilyID binds the persisted Entry.FamilyID column.
	// Example: orm.Set(Entries.FamilyID, familyID).
	FamilyID model.ID[Family] `foundry:"role=column"`
	// Generation binds the persisted Entry.Generation column.
	// Example: orm.Set(Entries.Generation, generation).
	Generation uint32
	// AccessHash binds the persisted Entry.AccessHash column.
	// Example: orm.Set(Entries.AccessHash, accessHash).
	AccessHash string
	// RefreshHash binds the persisted Entry.RefreshHash column.
	// Example: orm.SetNull(Entries.RefreshHash).
	RefreshHash value.Nullable[string]
	// IssuedAt binds the persisted Entry.IssuedAt column.
	// Example: orm.Set(Entries.IssuedAt, issuedAt).
	IssuedAt temporal.DateTime
	// LastSeenAt binds the persisted Entry.LastSeenAt column.
	// Example: orm.Set(Entries.LastSeenAt, lastSeenAt).
	LastSeenAt temporal.DateTime
	// AccessExpiresAt binds the persisted Entry.AccessExpiresAt column.
	// Example: orm.Set(Entries.AccessExpiresAt, accessExpiresAt).
	AccessExpiresAt temporal.DateTime
	// RefreshExpiresAt binds the persisted Entry.RefreshExpiresAt column.
	// Example: orm.SetNull(Entries.RefreshExpiresAt).
	RefreshExpiresAt value.Nullable[temporal.DateTime]
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("token subject")) }
func (Family) Format(s fmt.State, _ rune)  { _, _ = s.Write([]byte("stored token family")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored token generation")) }
