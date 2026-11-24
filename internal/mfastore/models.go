// Package mfastore owns typed persistence models for MFA. Applications use
// auth/mfa's typed manager instead of depending on these implementation rows.
package mfastore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// The application model is locked before this row, including when absent.
// Generation changes whenever a pending enrollment is replaced.
//
//foundry:model table=foundry_mfa_factors var=Factors key=Key identity=false
type Factor struct {
	// Table provides Factor queries and writes. Example: Factors.Query().Limit(10).
	Key string
	// Scope binds the persisted Factor.Scope column.
	// Example: orm.Set(Factors.Scope, scope).
	Scope string
	// Identity binds the persisted Factor.Identity column.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Factors.Identity, encoded) }.
	Identity value.JSON[model.Identity]
	// Generation binds the persisted Factor.Generation column.
	// Example: orm.Set(Factors.Generation, generation).
	Generation model.ID[Factor] `foundry:"role=column"`
	// Ciphertext binds the persisted Factor.Ciphertext column.
	// Example: orm.Set(Factors.Ciphertext, ciphertext).
	Ciphertext string
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: orm.Set(Factors.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
	// PendingUntil binds the persisted Factor.PendingUntil column.
	// Example: orm.SetNull(Factors.PendingUntil).
	PendingUntil value.Nullable[temporal.DateTime]
	// ConfirmedAt binds the persisted Factor.ConfirmedAt column.
	// Example: orm.SetNull(Factors.ConfirmedAt).
	ConfirmedAt value.Nullable[temporal.DateTime]
	// LastStep binds the persisted Factor.LastStep column.
	// Example: orm.SetNull(Factors.LastStep).
	LastStep value.Nullable[int64]
	// RecoveryHashes binds the persisted Factor.RecoveryHashes column.
	// Example: encoded, err := value.NewJSON(recoveryHashes); if err == nil { _ = orm.Set(Factors.RecoveryHashes, encoded) }.
	RecoveryHashes value.JSON[[]string]
}

func (Factor) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("stored MFA factor")) }
