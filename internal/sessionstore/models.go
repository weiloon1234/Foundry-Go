// Package sessionstore owns PostgreSQL session rows. Public consumers use typed
// session handles, never these heterogeneous infrastructure models.
package sessionstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Subject is the stable per-scope identity row used to serialize issue/revoke.
// It is not a user model and stores no roles, permissions or credentials.
//
//foundry:model table=foundry_session_subjects var=Subjects key=Key identity=false
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

// Entry contains only a hash of the random session secret. Rotation changes the
// hash while retaining the public session identifier and absolute deadline.
//
//foundry:model table=foundry_sessions var=Entries key=ID identity=false
type Entry struct {
	// Table provides Entry queries and writes. Example: Entries.Query().Limit(10).
	//
	// ID binds the persisted Entry.ID column.
	// Example: orm.Eq(Entries.ID, id).
	ID model.ID[Entry] `foundry:"member=ID"`
	// Scope binds the persisted Entry.Scope column.
	// Example: orm.Set(Entries.Scope, scope).
	Scope string
	// SubjectKey binds the persisted Entry.SubjectKey column.
	// Example: orm.Set(Entries.SubjectKey, subjectKey).
	SubjectKey string
	// SecretHash binds the persisted Entry.SecretHash column.
	// Example: orm.Set(Entries.SecretHash, secretHash).
	SecretHash string
	// Assurance binds the persisted Entry.Assurance column.
	// Example: orm.Set(Entries.Assurance, assurance).
	Assurance uint8
	// Remember binds the persisted Entry.Remember column.
	// Example: orm.Set(Entries.Remember, remember).
	Remember bool `foundry:"role=column"`
	// Sliding binds the persisted Entry.Sliding column.
	// Example: orm.Set(Entries.Sliding, sliding).
	Sliding bool `foundry:"role=column"`
	// IdleNanos binds the persisted Entry.IdleNanos column.
	// Example: orm.Set(Entries.IdleNanos, idleNanos).
	IdleNanos int64
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: orm.Set(Entries.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
	// LastSeenAt binds the persisted Entry.LastSeenAt column.
	// Example: orm.Set(Entries.LastSeenAt, lastSeenAt).
	LastSeenAt temporal.DateTime
	// IdleExpiresAt binds the persisted Entry.IdleExpiresAt column.
	// Example: orm.Set(Entries.IdleExpiresAt, idleExpiresAt).
	IdleExpiresAt temporal.DateTime
	// ExpiresAt binds the persisted Entry.ExpiresAt column.
	// Example: orm.Set(Entries.ExpiresAt, expiresAt).
	ExpiresAt temporal.DateTime
	// ClientIP and UserAgent are display-only device metadata from issuance.
	//
	// ClientIP binds the persisted Entry.ClientIP column.
	// Example: orm.SetNull(Entries.ClientIP).
	ClientIP value.Nullable[string]
	// UserAgent binds the persisted Entry.UserAgent column.
	// Example: orm.SetNull(Entries.UserAgent).
	UserAgent value.Nullable[string]
	// ConfirmedAt is the last password confirmation within this session.
	//
	// ConfirmedAt binds the persisted Entry.ConfirmedAt column.
	// Example: orm.SetNull(Entries.ConfirmedAt).
	ConfirmedAt value.Nullable[temporal.DateTime]
	// ImpersonatorIdentity (identity JSON), ImpersonatorGuard and
	// ImpersonatorSession mark an impersonation session with its original actor.
	//
	// ImpersonatorIdentity binds the persisted Entry.ImpersonatorIdentity column.
	// Example: orm.SetNull(Entries.ImpersonatorIdentity).
	ImpersonatorIdentity value.Nullable[string]
	// ImpersonatorGuard binds the persisted Entry.ImpersonatorGuard column.
	// Example: orm.SetNull(Entries.ImpersonatorGuard).
	ImpersonatorGuard value.Nullable[string]
	// ImpersonatorSession binds the persisted Entry.ImpersonatorSession column.
	// Example: orm.SetNull(Entries.ImpersonatorSession).
	ImpersonatorSession value.Nullable[model.ID[Entry]] `foundry:"role=column"`
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("session subject")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored session")) }
