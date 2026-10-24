// Package sessionstore owns PostgreSQL session rows. Public consumers use typed
// session handles, never these heterogeneous infrastructure models.
package sessionstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Subject is the stable per-scope identity row used to serialize issue/revoke.
// It is not a user model and stores no roles, permissions or credentials.
type Subject struct {
	Key      string
	Scope    string
	Identity value.JSON[model.Identity]
}

// Entry contains only a hash of the random session secret. Rotation changes the
// hash while retaining the public session identifier and absolute deadline.
type Entry struct {
	ID            model.ID[Entry]
	Scope         string
	SubjectKey    string
	SecretHash    string
	Assurance     uint8
	Remember      bool
	Sliding       bool
	IdleNanos     int64
	CreatedAt     temporal.DateTime
	LastSeenAt    temporal.DateTime
	IdleExpiresAt temporal.DateTime
	ExpiresAt     temporal.DateTime
	// ClientIP and UserAgent are display-only device metadata from issuance.
	ClientIP  value.Nullable[string]
	UserAgent value.Nullable[string]
	// ConfirmedAt is the last password confirmation within this session.
	ConfirmedAt value.Nullable[temporal.DateTime]
	// ImpersonatorIdentity (identity JSON), ImpersonatorGuard and
	// ImpersonatorSession mark an impersonation session with its original actor.
	ImpersonatorIdentity value.Nullable[string]
	ImpersonatorGuard    value.Nullable[string]
	ImpersonatorSession  value.Nullable[model.ID[Entry]]
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("session subject")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored session")) }

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
var Subjects = orm.Define("foundry_session_subjects", func(b orm.Builder[Subject], m *Subject) SubjectTable {
	table := orm.TableOf(b, &m.Key)
	return SubjectTable{Table: table,
		Scope:    orm.TextOf(b, &m.Scope),
		Identity: orm.JSONOf(b, &m.Identity),
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
	// SubjectKey binds the persisted Entry.SubjectKey column.
	// Example: Entries.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[Entry, string]
	// SecretHash binds the persisted Entry.SecretHash column.
	// Example: Entries.SecretHash.Set(secretHash).
	SecretHash orm.Text[Entry, string]
	// Assurance binds the persisted Entry.Assurance column.
	// Example: Entries.Assurance.Set(assurance).
	Assurance orm.Number[Entry, uint8]
	// Remember binds the persisted Entry.Remember column.
	// Example: Entries.Remember.Set(remember).
	Remember orm.Column[Entry, bool]
	// Sliding binds the persisted Entry.Sliding column.
	// Example: Entries.Sliding.Set(sliding).
	Sliding orm.Column[Entry, bool]
	// IdleNanos binds the persisted Entry.IdleNanos column.
	// Example: Entries.IdleNanos.Set(idleNanos).
	IdleNanos orm.Number[Entry, int64]
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: Entries.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Entry, temporal.DateTime]
	// LastSeenAt binds the persisted Entry.LastSeenAt column.
	// Example: Entries.LastSeenAt.Set(lastSeenAt).
	LastSeenAt orm.Time[Entry, temporal.DateTime]
	// IdleExpiresAt binds the persisted Entry.IdleExpiresAt column.
	// Example: Entries.IdleExpiresAt.Set(idleExpiresAt).
	IdleExpiresAt orm.Time[Entry, temporal.DateTime]
	// ExpiresAt binds the persisted Entry.ExpiresAt column.
	// Example: Entries.ExpiresAt.Set(expiresAt).
	ExpiresAt orm.Time[Entry, temporal.DateTime]
	// ClientIP binds the persisted Entry.ClientIP column.
	// Example: Entries.ClientIP.SetNull().
	ClientIP orm.NullableText[Entry, string]
	// UserAgent binds the persisted Entry.UserAgent column.
	// Example: Entries.UserAgent.SetNull().
	UserAgent orm.NullableText[Entry, string]
	// ConfirmedAt binds the persisted Entry.ConfirmedAt column.
	// Example: Entries.ConfirmedAt.SetNull().
	ConfirmedAt orm.NullableTime[Entry, temporal.DateTime]
	// ImpersonatorIdentity binds the persisted Entry.ImpersonatorIdentity column.
	// Example: Entries.ImpersonatorIdentity.SetNull().
	ImpersonatorIdentity orm.NullableText[Entry, string]
	// ImpersonatorGuard binds the persisted Entry.ImpersonatorGuard column.
	// Example: Entries.ImpersonatorGuard.SetNull().
	ImpersonatorGuard orm.NullableText[Entry, string]
	// ImpersonatorSession binds the persisted Entry.ImpersonatorSession column.
	// Example: Entries.ImpersonatorSession.SetNull().
	ImpersonatorSession orm.NullableColumn[Entry, model.ID[Entry]]
}

// Entries owns the immutable Entry declaration. Example: Entries.Query().First(ctx, tx).
var Entries = orm.Define("foundry_sessions", func(b orm.Builder[Entry], m *Entry) EntryTable {
	table := orm.TableOf(b, &m.ID)
	return EntryTable{Table: table,
		ID:                   table.Key(),
		Scope:                orm.TextOf(b, &m.Scope),
		SubjectKey:           orm.TextOf(b, &m.SubjectKey),
		SecretHash:           orm.TextOf(b, &m.SecretHash),
		Assurance:            orm.NumberOf(b, &m.Assurance),
		Remember:             orm.ColumnOf(b, &m.Remember),
		Sliding:              orm.ColumnOf(b, &m.Sliding),
		IdleNanos:            orm.NumberOf(b, &m.IdleNanos),
		CreatedAt:            orm.TimeOf(b, &m.CreatedAt),
		LastSeenAt:           orm.TimeOf(b, &m.LastSeenAt),
		IdleExpiresAt:        orm.TimeOf(b, &m.IdleExpiresAt),
		ExpiresAt:            orm.TimeOf(b, &m.ExpiresAt),
		ClientIP:             orm.NullableTextOf(b, &m.ClientIP),
		UserAgent:            orm.NullableTextOf(b, &m.UserAgent),
		ConfirmedAt:          orm.NullableTimeOf(b, &m.ConfirmedAt),
		ImpersonatorIdentity: orm.NullableTextOf(b, &m.ImpersonatorIdentity),
		ImpersonatorGuard:    orm.NullableTextOf(b, &m.ImpersonatorGuard),
		ImpersonatorSession:  orm.NullableColumnOf(b, &m.ImpersonatorSession),
	}
})
