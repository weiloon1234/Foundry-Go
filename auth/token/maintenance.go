package token

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func (t *Tokens[M, K]) Revoke(ctx context.Context, access secret.String) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	hash, err := HashSecret(access)
	if err != nil {
		return false, err
	}
	var removed bool
	err = t.store.execute(ctx, func(op context.Context) error {
		var err error
		removed, err = t.store.backend.Revoke(op, t.address, hash)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
func (t *Tokens[M, K]) identity(reference model.Reference[M, K]) (model.Identity, error) {
	identity, err := reference.Identity()
	if err != nil {
		return model.Identity{}, err
	}
	if _, err := t.provider.Parse(identity); err != nil {
		return model.Identity{}, err
	}
	return identity, nil
}

// RevokeID removes one family belonging to this exact model subject and guard.
// Authorize the caller before supplying a subject; the ID is not authority.
func (t *Tokens[M, K]) RevokeID(ctx context.Context, reference model.Reference[M, K], id ID[M]) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	if id.IsZero() {
		return false, fault.New(fault.Invalid, "token revocation requires an identifier")
	}
	var removed bool
	err := t.store.execute(ctx, func(op context.Context) error {
		identity, err := t.identity(reference)
		if err != nil {
			return err
		}
		removed, err = t.store.backend.RevokeID(op, t.address, identity, id.value)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
func (t *Tokens[M, K]) RevokeAll(ctx context.Context, reference model.Reference[M, K]) (uint64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	var count uint64
	err := t.store.execute(ctx, func(op context.Context) error {
		identity, err := t.identity(reference)
		if err != nil {
			return err
		}
		count, err = t.store.backend.RevokeAll(op, t.address, identity)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// List returns bounded current-family metadata. An expired access credential can
// still have a live refresh grant; callers can inspect both expiry values.
func (t *Tokens[M, K]) List(ctx context.Context, reference model.Reference[M, K]) ([]Info[M, K], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	var result []Info[M, K]
	err := t.store.execute(ctx, func(op context.Context) error {
		identity, err := t.identity(reference)
		if err != nil {
			return err
		}
		records, err := t.store.backend.List(op, t.address, identity, MaxTokens)
		if err != nil {
			return err
		}
		if len(records) > MaxTokens {
			return fault.New(fault.Invalid, "token backend exceeded result limit")
		}
		result = make([]Info[M, K], len(records))
		seen := make(map[model.ID[Record]]bool, len(records))
		for i, record := range records {
			if record.Subject != identity || seen[record.ID] {
				return fault.New(fault.Invalid, "token backend returned foreign or duplicate metadata")
			}
			seen[record.ID] = true
			result[i], err = t.info(record)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Prune removes at most limit expired families with their bounded generation
// history. Stable subject rows remain for issuance/revocation serialization.
func (t *Tokens[M, K]) Prune(ctx context.Context, limit int) (uint64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPruneFamilies {
		return 0, fault.New(fault.Invalid, "invalid token prune family limit")
	}
	var count uint64
	err := t.store.execute(ctx, func(op context.Context) error {
		var err error
		count, err = t.store.backend.Prune(op, t.address, limit)
		if err == nil && count > uint64(limit) {
			return fault.New(fault.Invalid, "token backend exceeded prune limit")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Touch records explicit access activity without extending access, refresh or
// absolute expiry. Ordinary guard verification remains read-only.
func (t *Tokens[M, K]) Touch(ctx context.Context, access secret.String) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	hash, err := HashSecret(access)
	if err != nil {
		return false, err
	}
	var present bool
	err = t.store.execute(ctx, func(op context.Context) error {
		found, err := t.store.backend.Lookup(op, t.address, hash, true)
		if err != nil {
			return err
		}
		record, ok := found.Get()
		if !ok {
			return nil
		}
		if !record.AccessHash.Equal(hash) {
			return fault.New(fault.Invalid, "token backend touched a different credential")
		}
		if _, err := t.info(record); err != nil {
			return err
		}
		present = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return present, nil
}
