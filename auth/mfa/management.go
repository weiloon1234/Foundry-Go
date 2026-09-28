package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Disable requires a rechecked password, current TOTP/recovery factor and domain
// permission. Factor removal, model mutation and credential revocation share tx.
func (f *Factors[M, K]) Disable(ctx context.Context, password auth.PasswordResult[M, K], response Response) (M, error) {
	if err := response.Validate(); err != nil {
		return *new(M), err
	}
	var result M
	err := f.withPassword(ctx, password, func(op context.Context, tx *database.Tx, current M, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
		record, err := f.active(current, stored, now)
		if err != nil {
			return Change{}, err
		}
		allowed, err := f.model.CanDisable(op, current)
		if err != nil {
			return Change{}, err
		}
		if !allowed {
			return Change{}, auth.Forbidden
		}
		if _, err := f.verify(op, record, response, now); err != nil {
			return Change{}, err
		}
		result, err = f.setEnabled(op, tx, current, false)
		if err != nil {
			return Change{}, err
		}
		if err := f.changed(op, tx, record.Subject, Disabled); err != nil {
			return Change{}, err
		}
		return Remove(), nil
	})
	if err != nil {
		return *new(M), err
	}
	return result, nil
}

// RegenerateRecovery verifies a current factor, replaces the complete recovery
// set and revokes registered credentials atomically. No old recovery code survives.
func (f *Factors[M, K]) RegenerateRecovery(ctx context.Context, password auth.PasswordResult[M, K], response Response) (RecoveryCodes[M], error) {
	if err := response.Validate(); err != nil {
		return RecoveryCodes[M]{}, err
	}
	var result RecoveryCodes[M]
	err := f.withPassword(ctx, password, func(op context.Context, tx *database.Tx, current M, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
		record, err := f.active(current, stored, now)
		if err != nil {
			return Change{}, err
		}
		record, err = f.verify(op, record, response, now)
		if err != nil {
			return Change{}, err
		}
		codes, hashes, err := newRecoveryCodes(f.store.config.RecoveryCodes)
		if err != nil {
			return Change{}, err
		}
		record.RecoveryHashes = hashes
		if err := f.model.Invalidate(op, tx, current); err != nil {
			return Change{}, err
		}
		if err := f.changed(op, tx, record.Subject, RecoveryRegenerated); err != nil {
			return Change{}, err
		}
		result = RecoveryCodes[M]{subject: current, id: EnrollmentID[M]{id: record.ID}, codes: codes}
		return Replace(record), nil
	})
	if err != nil {
		return RecoveryCodes[M]{}, err
	}
	return result, nil
}

// Reencrypt requires password/factor verification, preserves the enrollment ID
// and recovery set (apart from the code used), and encrypts with the active key.
// It does not change MFA policy or issue credentials. Retain old keys until every
// stored factor has migrated; this operation cannot recover a lost key.
func (f *Factors[M, K]) Reencrypt(ctx context.Context, password auth.PasswordResult[M, K], response Response) error {
	if err := response.Validate(); err != nil {
		return err
	}
	return f.withPassword(ctx, password, func(op context.Context, tx *database.Tx, current M, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
		record, err := f.active(current, stored, now)
		if err != nil {
			return Change{}, err
		}
		record, err = f.verify(op, record, response, now)
		if err != nil {
			return Change{}, err
		}
		owner, err := record.encryptionContext()
		if err != nil {
			return Change{}, err
		}
		record.Ciphertext, err = f.store.keys.Reencrypt(op, owner, record.Ciphertext)
		if err != nil {
			return Change{}, err
		}
		if err := f.changed(op, tx, record.Subject, Reencrypted); err != nil {
			return Change{}, err
		}
		return Replace(record), nil
	})
}

// Prune removes a bounded batch of expired, unconfirmed enrollments only.
// Confirmed factors and application models are never deleted by maintenance.
func (f *Factors[M, K]) Prune(ctx context.Context, limit int) (uint64, error) {
	if err := f.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPrune {
		return 0, fault.New(fault.Invalid, "invalid MFA prune limit")
	}
	var removed uint64
	err := f.store.gate.Execute(ctx, func(op context.Context) error {
		var err error
		removed, err = f.store.backend.Prune(op, f.address, limit)
		if err == nil && removed > uint64(limit) {
			return fault.New(fault.Invalid, "MFA prune exceeded its limit")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
