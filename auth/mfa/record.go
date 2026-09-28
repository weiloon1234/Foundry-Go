package mfa

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Record is the trusted storage-adapter boundary, never a transport DTO. A
// pending record has an expiry but no confirmation, replay step or recovery
// hashes. A confirmed record has no pending expiry. ID changes on reenrollment.
type Record struct {
	ID             model.ID[Record]
	Address        Address
	Subject        model.Identity
	Ciphertext     encryption.Ciphertext
	CreatedAt      temporal.DateTime
	PendingUntil   value.Optional[temporal.DateTime]
	ConfirmedAt    value.Optional[temporal.DateTime]
	LastStep       value.Optional[int64]
	RecoveryHashes []RecoveryHash
}

func (r Record) Clone() Record              { r.RecoveryHashes = slices.Clone(r.RecoveryHashes); return r }
func (Record) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("stored MFA factor")) }
func (Record) LogValue() slog.Value         { return slog.StringValue("stored MFA factor") }
func (Record) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (r Record) Validate(address Address, subject model.Identity) error {
	if _, err := address.SubjectKey(subject); err != nil {
		return err
	}
	if r.Address != address || r.Subject != subject || r.ID.IsZero() || r.Ciphertext.IsZero() || len(r.Ciphertext.Encoded()) > 512 || !validInstant(r.CreatedAt) {
		return fault.New(fault.Invalid, "invalid stored MFA factor")
	}
	pending, isPending := r.PendingUntil.Get()
	confirmed, isConfirmed := r.ConfirmedAt.Get()
	step, used := r.LastStep.Get()
	if isPending == isConfirmed {
		return fault.New(fault.Invalid, "MFA factor must be pending or confirmed")
	}
	if isPending {
		if !validInstant(pending) || used || len(r.RecoveryHashes) != 0 {
			return fault.New(fault.Invalid, "invalid pending MFA factor")
		}
		return validateEnrollmentLifetime(pending.UTC().Sub(r.CreatedAt.UTC()))
	}
	if !validInstant(confirmed) || confirmed.UTC().Before(r.CreatedAt.UTC()) || !used || step < 0 || step > 253402300799/totpPeriodSeconds {
		return fault.New(fault.Invalid, "invalid confirmed MFA factor")
	}
	return validateRecoveryHashes(r.RecoveryHashes)
}
func validInstant(now temporal.DateTime) bool {
	return now.UTC().Unix() >= 0 && now.UTC().Nanosecond()%1000 == 0
}
func (r Record) same(other Record) bool {
	return r.ID == other.ID && r.Address == other.Address && r.Subject == other.Subject && r.Ciphertext == other.Ciphertext && r.CreatedAt == other.CreatedAt && r.PendingUntil == other.PendingUntil && r.ConfirmedAt == other.ConfirmedAt && r.LastStep == other.LastStep && slices.Equal(r.RecoveryHashes, other.RecoveryHashes)
}
func (r Record) encryptionContext() (encryption.Context, error) {
	owner, err := r.Address.SubjectKey(r.Subject)
	if err != nil {
		return encryption.Context{}, err
	}
	return encryption.NewContext("auth.mfa.totp.v1", secret.New(owner+":"+r.ID.String()))
}
