package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/mfastore"
)

func (b *Backend) Prune(ctx context.Context, address mfa.Address, limit int) (uint64, error) {
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > mfa.MaxPrune {
		return 0, fault.New(fault.Invalid, "invalid MFA prune limit")
	}
	var removed uint64
	err = b.within(ctx, func(tx *database.Tx) error {
		now, err := b.now()
		if err != nil {
			return err
		}
		fields := mfastore.FactorFields()
		rows, err := factors(scope).Where(fields.PendingUntil.Lte(now)).OrderBy(fields.Key.Asc()).Limit(limit).ForUpdate().SkipLocked().All(ctx, tx)
		if err != nil {
			return err
		}
		// Pruning never locks a model or credential row, avoiding inverse lock order.
		for _, row := range rows {
			record, err := decode(address, row)
			if err != nil {
				return err
			}
			expiry, pending := record.PendingUntil.Get()
			if !pending || expiry.UTC().After(now.UTC()) {
				continue
			}
			if _, err := factors(scope).Delete(ctx, tx, row.Key); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
