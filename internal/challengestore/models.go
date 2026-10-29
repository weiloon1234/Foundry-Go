// Package challengestore owns recovery credentials; applications use typed
// auth/passwordreset and auth/emailverification flows instead of these rows.
package challengestore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Subject struct {
	Key      string
	Scope    string
	Identity value.JSON[model.Identity]
}

// Entry has one current credential per stable subject/purpose. Reissuance creates
// a new ID so a stale pruning candidate cannot delete the replacement.
type Entry struct {
	ID          model.ID[Entry]
	Scope       string
	SubjectKey  string
	SecretHash  string
	BindingHash string
	CreatedAt   temporal.DateTime
	ExpiresAt   temporal.DateTime
}

type PruneCandidate struct {
	ID         model.ID[Entry]
	SubjectKey string
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("challenge subject")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored challenge")) }

// SubjectTable binds the persisted Subject columns. Example: Subjects.Query().Limit(10).
type SubjectTable struct {
	// Table provides Subject queries and writes. Example: Subjects.Query().Limit(10).
	orm.Table[Subject, string]
	// Scope binds the persisted Subject.Scope column.
	// Example: orm.TextSet(Subjects.Scope, scope).
	Scope orm.Text[Subject, string]
	// Identity binds the persisted Subject.Identity column.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.JSONSet(Subjects.Identity, encoded) }.
	Identity orm.JSON[Subject, model.Identity]
}

// Subjects owns the immutable Subject declaration. Example: orm.First(ctx, tx, Subjects.Query()).
var Subjects = orm.Define("foundry_challenge_subjects", func(b orm.Builder[Subject], m *Subject) SubjectTable {
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
	// Example: orm.KeyEq(Entries.ID, id).
	ID orm.Key[Entry, model.ID[Entry]]
	// Scope binds the persisted Entry.Scope column.
	// Example: orm.TextSet(Entries.Scope, scope).
	Scope orm.Text[Entry, string]
	// SubjectKey binds the persisted Entry.SubjectKey column.
	// Example: orm.TextSet(Entries.SubjectKey, subjectKey).
	SubjectKey orm.Text[Entry, string]
	// SecretHash binds the persisted Entry.SecretHash column.
	// Example: orm.TextSet(Entries.SecretHash, secretHash).
	SecretHash orm.Text[Entry, string]
	// BindingHash binds the persisted Entry.BindingHash column.
	// Example: orm.TextSet(Entries.BindingHash, bindingHash).
	BindingHash orm.Text[Entry, string]
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: orm.TimeSet(Entries.CreatedAt, createdAt).
	CreatedAt orm.Time[Entry, temporal.DateTime]
	// ExpiresAt binds the persisted Entry.ExpiresAt column.
	// Example: orm.TimeSet(Entries.ExpiresAt, expiresAt).
	ExpiresAt orm.Time[Entry, temporal.DateTime]
}

// Entries owns the immutable Entry declaration. Example: orm.First(ctx, tx, Entries.Query()).
var Entries = orm.Define("foundry_challenges", func(b orm.Builder[Entry], m *Entry) EntryTable {
	table := orm.TableOf(b, &m.ID)
	return EntryTable{Table: table,
		ID:          table.Key(),
		Scope:       orm.TextOf(b, &m.Scope),
		SubjectKey:  orm.TextOf(b, &m.SubjectKey),
		SecretHash:  orm.TextOf(b, &m.SecretHash),
		BindingHash: orm.TextOf(b, &m.BindingHash),
		CreatedAt:   orm.TimeOf(b, &m.CreatedAt),
		ExpiresAt:   orm.TimeOf(b, &m.ExpiresAt),
	}
})

// PruneCandidateRows declares the narrow pruning result. Example: rows.Select(source, PruneCandidateRows, mappings...).
var PruneCandidateRows = rows.Define(func(b rows.Builder[PruneCandidate], r *PruneCandidate) struct {
	// Row owns the pruning projection. Example: rows.Select(source, PruneCandidateRows, mappings...).
	rows.Row[PruneCandidate]
	// ID receives the pruning candidate's ID.
	// Example: rows.Map(PruneCandidateRows.ID, expr.Value(Entries.ID)).
	ID rows.Field[PruneCandidate, model.ID[Entry]]
	// SubjectKey receives the pruning candidate's SubjectKey.
	// Example: rows.Map(PruneCandidateRows.SubjectKey, expr.Value(Entries.SubjectKey)).
	SubjectKey rows.Field[PruneCandidate, string]
} {
	return struct {
		// Row owns the pruning projection. Example: rows.Select(source, PruneCandidateRows, mappings...).
		rows.Row[PruneCandidate]
		// ID receives the pruning candidate's ID.
		// Example: rows.Map(PruneCandidateRows.ID, expr.Value(Entries.ID)).
		ID rows.Field[PruneCandidate, model.ID[Entry]]
		// SubjectKey receives the pruning candidate's SubjectKey.
		// Example: rows.Map(PruneCandidateRows.SubjectKey, expr.Value(Entries.SubjectKey)).
		SubjectKey rows.Field[PruneCandidate, string]
	}{Row: b.Row(), ID: rows.IDOf(b, &r.ID), SubjectKey: rows.TextOf(b, &r.SubjectKey)}
})
