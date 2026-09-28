// Package mfastore owns generated persistence models for MFA. Applications use
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
//foundry:model table=foundry_mfa_factors primary=Key
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
