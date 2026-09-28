package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Enroll replaces only an unconfirmed enrollment. The password result is
// rechecked under the model lock. An enabled/confirmed factor must be verified
// and disabled first; an ordinary session or bare model reference cannot enroll.
func (f *Factors[M, K]) Enroll(ctx context.Context, password auth.PasswordResult[M, K]) (Enrollment[M], error) {
	var result Enrollment[M]
	err := f.withPassword(ctx, password, func(op context.Context, tx *database.Tx, current M, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
		if f.model.Enabled(current) {
			return Change{}, fault.New(fault.Conflict, "MFA is already enabled")
		}
		if previous, present := stored.Get(); present && previous.ConfirmedAt.IsSet() {
			return Change{}, fault.New(fault.Conflict, "confirmed MFA factor cannot be replaced by enrollment")
		}
		label, err := f.model.AccountLabel(current)
		if err != nil {
			return Change{}, err
		}
		key, err := GenerateTOTPSecret()
		if err != nil {
			return Change{}, err
		}
		uri, err := ProvisioningURI(key, f.store.config.Issuer, label)
		if err != nil {
			return Change{}, err
		}
		id, err := model.NewIDAt[Record](now.UTC())
		if err != nil {
			return Change{}, err
		}
		expiry, err := now.Add(f.store.config.EnrollmentLifetime)
		if err != nil {
			return Change{}, err
		}
		identity, err := current.FoundryIdentity()
		if err != nil {
			return Change{}, err
		}
		record := Record{ID: id, Address: f.address, Subject: identity, CreatedAt: now, PendingUntil: value.Set(expiry)}
		owner, err := record.encryptionContext()
		if err != nil {
			return Change{}, err
		}
		record.Ciphertext, err = f.store.keys.Encrypt(op, owner, key.Secret())
		if err != nil {
			return Change{}, err
		}
		result = Enrollment[M]{subject: current, id: EnrollmentID[M]{id: id}, key: key, uri: uri, created: now, expires: expiry}
		return Replace(record).Before(expiry), nil
	})
	if err != nil {
		return Enrollment[M]{}, err
	}
	return result, nil
}

// Confirm accepts only a TOTP code for the current unexpired enrollment. The
// accepted step, recovery hashes, model flag and all credential revocations
// commit together. The result is not an authenticated proof or a login response.
func (f *Factors[M, K]) Confirm(ctx context.Context, password auth.PasswordResult[M, K], id EnrollmentID[M], code TOTPCode) (RecoveryCodes[M], error) {
	if id.IsZero() {
		return RecoveryCodes[M]{}, fault.New(fault.Invalid, "enrollment ID is empty")
	}
	response, err := TOTPResponse(code)
	if err != nil {
		return RecoveryCodes[M]{}, err
	}
	var result RecoveryCodes[M]
	err = f.withPassword(ctx, password, func(op context.Context, tx *database.Tx, current M, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
		record, present := stored.Get()
		if !present || record.ID != id.id || f.model.Enabled(current) || record.ConfirmedAt.IsSet() {
			return Change{}, auth.Unauthenticated
		}
		expiry, pending := record.PendingUntil.Get()
		if !pending || !now.UTC().Before(expiry.UTC()) {
			return Change{}, auth.Unauthenticated
		}
		record, err := f.verify(op, record, response, now)
		if err != nil {
			return Change{}, err
		}
		codes, hashes, err := newRecoveryCodes(f.store.config.RecoveryCodes)
		if err != nil {
			return Change{}, err
		}
		record.PendingUntil = value.Optional[temporal.DateTime]{}
		record.ConfirmedAt = value.Set(now)
		record.RecoveryHashes = hashes
		updated, err := f.setEnabled(op, tx, current, true)
		if err != nil {
			return Change{}, err
		}
		if err := f.changed(op, tx, record.Subject, Enrolled); err != nil {
			return Change{}, err
		}
		result = RecoveryCodes[M]{subject: updated, id: id, codes: codes}
		return Replace(record).Before(expiry), nil
	})
	if err != nil {
		return RecoveryCodes[M]{}, err
	}
	return result, nil
}
