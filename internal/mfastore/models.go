// Package mfastore owns typed persistence models for MFA. Applications use
// auth/mfa's typed manager instead of depending on these implementation rows.
package mfastore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// The application model is locked before this row, including when absent.
// Generation changes whenever a pending enrollment is replaced.
type Factor struct {
	Key            string
	Scope          string
	Identity       value.JSON[model.Identity]
	Generation     model.ID[Factor]
	Ciphertext     string
	CreatedAt      temporal.DateTime
	PendingUntil   value.Nullable[temporal.DateTime]
	ConfirmedAt    value.Nullable[temporal.DateTime]
	LastStep       value.Nullable[int64]
	RecoveryHashes value.JSON[[]string]
}

func (Factor) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("stored MFA factor")) }

// FactorTable binds the persisted Factor columns. Example: Factors.Query().Limit(10).
type FactorTable struct {
	// Table provides Factor queries and writes. Example: Factors.Query().Limit(10).
	orm.Table[Factor, string]
	// Scope binds the persisted Factor.Scope column.
	// Example: Factors.Scope.Set(scope).
	Scope orm.Text[Factor, string]
	// Identity binds the persisted Factor.Identity column.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = Factors.Identity.Set(encoded) }.
	Identity orm.JSON[Factor, model.Identity]
	// Generation binds the persisted Factor.Generation column.
	// Example: Factors.Generation.Set(generation).
	Generation orm.Column[Factor, model.ID[Factor]]
	// Ciphertext binds the persisted Factor.Ciphertext column.
	// Example: Factors.Ciphertext.Set(ciphertext).
	Ciphertext orm.Text[Factor, string]
	// CreatedAt binds the caller-supplied creation time; the ORM does not automatically populate or refresh it.
	// Example: Factors.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Factor, temporal.DateTime]
	// PendingUntil binds the persisted Factor.PendingUntil column.
	// Example: Factors.PendingUntil.SetNull().
	PendingUntil orm.NullableTime[Factor, temporal.DateTime]
	// ConfirmedAt binds the persisted Factor.ConfirmedAt column.
	// Example: Factors.ConfirmedAt.SetNull().
	ConfirmedAt orm.NullableTime[Factor, temporal.DateTime]
	// LastStep binds the persisted Factor.LastStep column.
	// Example: Factors.LastStep.SetNull().
	LastStep orm.NullableNumber[Factor, int64]
	// RecoveryHashes binds the persisted Factor.RecoveryHashes column.
	// Example: encoded, err := value.NewJSON(recoveryHashes); if err == nil { _ = Factors.RecoveryHashes.Set(encoded) }.
	RecoveryHashes orm.JSON[Factor, []string]
}

// Factors owns the immutable Factor declaration. Example: Factors.Query().First(ctx, tx).
var Factors = orm.Define("foundry_mfa_factors", func(b orm.Builder[Factor], m *Factor) FactorTable {
	table := orm.TableOf(b, &m.Key)
	return FactorTable{Table: table,
		Scope:          orm.TextOf(b, &m.Scope),
		Identity:       orm.JSONOf(b, &m.Identity),
		Generation:     orm.ColumnOf(b, &m.Generation),
		Ciphertext:     orm.TextOf(b, &m.Ciphertext),
		CreatedAt:      orm.TimeOf(b, &m.CreatedAt),
		PendingUntil:   orm.NullableTimeOf(b, &m.PendingUntil),
		ConfirmedAt:    orm.NullableTimeOf(b, &m.ConfirmedAt),
		LastStep:       orm.NullableNumberOf(b, &m.LastStep),
		RecoveryHashes: orm.JSONOf(b, &m.RecoveryHashes),
	}
})
