// Package challengestore owns recovery credentials; applications use typed
// auth/passwordreset and auth/emailverification flows instead of these rows.
package challengestore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_challenge_subjects primary=Key
type Subject struct {
	Key      string
	Scope    string
	Identity value.JSON[model.Identity]
}

// Entry has one current credential per stable subject/purpose. Reissuance creates
// a new ID so a stale pruning candidate cannot delete the replacement.
//
//foundry:model table=foundry_challenges
type Entry struct {
	ID          model.ID[Entry]
	Scope       string
	SubjectKey  string
	SecretHash  string
	BindingHash string
	CreatedAt   temporal.DateTime
	ExpiresAt   temporal.DateTime
}

//foundry:projection
type PruneCandidate struct {
	ID         model.ID[Entry]
	SubjectKey string
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("challenge subject")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored challenge")) }
