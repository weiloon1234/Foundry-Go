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
//foundry:model table=foundry_token_subjects primary=Key
type Subject struct {
	Key      string
	Scope    string
	Identity value.JSON[model.Identity]
}

// Family owns identity, grant, immutable expiry policy and the current generation.
//
//foundry:model table=foundry_token_families
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
//
//foundry:model table=foundry_token_generations
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
