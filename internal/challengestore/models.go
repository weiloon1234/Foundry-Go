// Package challengestore owns recovery credentials; applications use typed
// auth/passwordreset and auth/emailverification flows instead of these rows.
package challengestore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_challenge_subjects var=Subjects key=Key identity=false
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

// Entry has one current credential per stable subject/purpose. Reissuance creates
// a new ID so a stale pruning candidate cannot delete the replacement.
//
//foundry:model table=foundry_challenges var=Entries key=ID identity=false
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
	// BindingHash binds the persisted Entry.BindingHash column.
	// Example: orm.Set(Entries.BindingHash, bindingHash).
	BindingHash string
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: orm.Set(Entries.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
	// ExpiresAt binds the persisted Entry.ExpiresAt column.
	// Example: orm.Set(Entries.ExpiresAt, expiresAt).
	ExpiresAt temporal.DateTime
}

type PruneCandidate struct {
	ID         model.ID[Entry]
	SubjectKey string
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("challenge subject")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored challenge")) }

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
