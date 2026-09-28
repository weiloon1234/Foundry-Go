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
//foundry:model table=foundry_session_subjects primary=Key
type Subject struct {
	Key      string
	Scope    string
	Identity value.JSON[model.Identity]
}

// Entry contains only a hash of the random session secret. Rotation changes the
// hash while retaining the public session identifier and absolute deadline.
//
//foundry:model table=foundry_sessions
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
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("session subject")) }
func (Entry) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored session")) }
