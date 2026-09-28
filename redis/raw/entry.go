package raw

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Exists checks a resource of any native Redis data type through its typed key.
func (k Keys[K]) Exists(ctx context.Context, input K) (bool, error) {
	var result bool
	err := k.store.execute(ctx, func(ctx context.Context) error {
		key, err := k.resolve(ctx, input)
		if err != nil {
			return err
		}
		result, err = NewCommand("EXISTS", DecodeBool()).Key(key).run(ctx, k.store)
		return err
	})
	if err != nil {
		return false, err
	}
	return result, nil
}
func (k Keys[K]) Expire(ctx context.Context, input K, ttl cache.TTL) (bool, error) {
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	var result bool
	err := k.store.execute(ctx, func(ctx context.Context) error {
		key, err := k.resolve(ctx, input)
		if err != nil {
			return err
		}
		result, err = k.store.backend.ExpireRaw(ctx, key, ttl)
		return err
	})
	if err != nil {
		return false, err
	}
	return result, nil
}
func (k Keys[K]) Delete(ctx context.Context, input K) (bool, error) {
	n, err := k.DeleteMany(ctx, input)
	return n == 1, err
}

// DeleteMany encodes all input keys before one atomic DEL and returns a count of
// distinct existing keys. It never enumerates namespaces or retries an uncertain DEL.
func (k Keys[K]) DeleteMany(ctx context.Context, inputs ...K) (uint64, error) {
	var count uint64
	err := k.store.execute(ctx, func(ctx context.Context) error {
		if len(inputs) > k.store.config.Limits.Arguments {
			return fault.New(fault.Invalid, "raw Redis deletion batch exceeds its bound")
		}
		if len(inputs) == 0 {
			return nil
		}
		var command Command[uint64]
		seen := make(map[string]bool, len(inputs))
		for _, input := range inputs {
			key, err := k.resolve(ctx, input)
			if err != nil {
				return err
			}
			if seen[key.String()] {
				continue
			}
			seen[key.String()] = true
			if len(seen) == 1 {
				command = NewCommand("DEL", DecodeUint64()).Key(key)
			} else {
				command = command.Key(key)
			}
		}
		var err error
		count, err = command.run(ctx, k.store)
		if err != nil {
			return err
		}
		if count > uint64(len(seen)) {
			return fault.New(fault.Internal, "invalid raw Redis deletion count")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
