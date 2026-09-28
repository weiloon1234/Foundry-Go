package mfa

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// verify changes only owned memory. Throttle.Run must finish successfully before
// callers write any protected state. Thus a late lockout cannot publish an
// action, consume a recovery code or advance the persisted replay step.
func (f *Factors[M, K]) verify(ctx context.Context, record Record, response Response, now temporal.DateTime) (Record, error) {
	if err := response.Validate(); err != nil {
		return Record{}, err
	}
	reference, err := f.provider.Parse(record.Subject)
	if err != nil {
		return Record{}, err
	}
	next := record.Clone()
	matched, err := f.attempts.Run(ctx, reference.Key(), func(op context.Context) (bool, error) {
		if response.kind == 2 {
			hashes, ok, err := consumeRecovery(record.RecoveryHashes, response.recovery)
			if err != nil || !ok {
				return false, err
			}
			next.RecoveryHashes = hashes
			return true, nil
		}
		owner, err := record.encryptionContext()
		if err != nil {
			return false, err
		}
		raw, err := f.store.keys.Decrypt(op, owner, record.Ciphertext)
		if err != nil {
			return false, err
		}
		key, err := ParseTOTPSecret(raw)
		if err != nil {
			return false, err
		}
		step, err := matchTOTP(key, response.code, now, f.store.config.Window, record.LastStep)
		if err != nil || !step.IsSet() {
			return false, err
		}
		next.LastStep = step
		return true, nil
	})
	if err != nil {
		if errorgraph.Has[*lockout.Rejection](err) {
			return Record{}, errors.Join(err, f.rejected(ctx, reference))
		}
		return Record{}, err
	}
	if !matched {
		return Record{}, errors.Join(auth.Unauthenticated, f.rejected(ctx, reference))
	}
	return next, nil
}
func (f *Factors[M, K]) active(current M, stored value.Optional[Record], now temporal.DateTime) (Record, error) {
	record, present := stored.Get()
	confirmed, enabled := record.ConfirmedAt.Get()
	if !present || !enabled || !f.model.Enabled(current) || now.UTC().Before(confirmed.UTC()) {
		return Record{}, auth.Unauthenticated
	}
	return record, nil
}
