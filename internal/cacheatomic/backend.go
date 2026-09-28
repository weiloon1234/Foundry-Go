// Package cacheatomic shares expiry, counter and mutation semantics for durable
// cache adapters. The driver serializes Access across processes for the same key.
package cacheatomic

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"slices"
	"time"
)

type Record struct {
	Data    []byte
	Expires time.Time
}

func (r *Record) Live(now time.Time) bool {
	return r != nil && (r.Expires.IsZero() || now.Before(r.Expires))
}

// Change runs while the driver owns its atomic lock. update=false retains bytes;
// update=true with nil next deletes. Errors preserve the previous entry.
type Change func(previous *Record) (next *Record, update bool, err error)

// Inspection runs after the driver acquires its lock and reads the record.
// It checks current liveness and optionally returns a replacement expiry.
// A nil replacement preserves metadata; false never revives an expired entry.
type Inspection func(expires time.Time) (replacement *time.Time, live bool, err error)

type Driver interface {
	Access(context.Context, cache.EntryKey, Change) error
	Inspect(context.Context, cache.EntryKey, Inspection) (bool, error)
}
type Backend struct {
	driver   Driver
	clock    clock.Clock
	maxValue int
}

func New(driver Driver, source clock.Clock, maxValue int) (*Backend, error) {
	if driver == nil || credential.IsNil(source) || !ValidValueLimit(maxValue) {
		return nil, fault.New(fault.Invalid, "invalid persistent cache configuration")
	}
	return &Backend{driver: driver, clock: source, maxValue: maxValue}, nil
}
func (b *Backend) validate(ctx context.Context, key cache.EntryKey) error {
	if b == nil || b.driver == nil || ctx == nil {
		return fault.New(fault.Invalid, "cache backend is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return key.Validate()
}
func (b *Backend) access(ctx context.Context, key cache.EntryKey, fn Change) error {
	if err := b.validate(ctx, key); err != nil {
		return err
	}
	return b.driver.Access(ctx, key, func(old *Record) (*Record, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if old != nil && len(old.Data) > b.maxValue {
			return nil, false, fault.New(fault.Invalid, "stored cache value exceeds limit")
		}
		if !old.Live(b.clock.Now()) {
			old = nil
		}
		next, update, err := fn(old)
		if err != nil {
			return nil, false, err
		}
		if next != nil && len(next.Data) > b.maxValue {
			return nil, false, fault.New(fault.Invalid, "cache value exceeds limit")
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		return next, update, nil
	})
}
func (b *Backend) record(data []byte, ttl cache.TTL) *Record {
	r := &Record{Data: slices.Clone(data)}
	if !ttl.IsForever() {
		r.Expires = b.clock.Now().Add(ttl.Duration())
	}
	return r
}
func (b *Backend) Get(ctx context.Context, key cache.EntryKey) (data []byte, hit bool, err error) {
	err = b.access(ctx, key, func(old *Record) (*Record, bool, error) {
		if old != nil {
			data = slices.Clone(old.Data)
			hit = true
		}
		return nil, false, nil
	})
	if err != nil {
		return nil, false, err
	}
	return
}
func (b *Backend) Put(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) error {
	_, err := b.write(ctx, key, data, ttl, false)
	return err
}
func (b *Backend) Add(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) (bool, error) {
	return b.write(ctx, key, data, ttl, true)
}
func (b *Backend) write(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL, absent bool) (bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return false, err
	}
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	if len(data) > b.maxValue {
		return false, fault.New(fault.Invalid, "cache value exceeds limit")
	}
	changed := false
	err := b.access(ctx, key, func(old *Record) (*Record, bool, error) {
		if absent && old != nil {
			return nil, false, nil
		}
		changed = true
		return b.record(data, ttl), true, nil
	})
	return changed && err == nil, err
}
func (b *Backend) Forget(ctx context.Context, key cache.EntryKey) (bool, error) {
	removed := false
	err := b.access(ctx, key, func(old *Record) (*Record, bool, error) { removed = old != nil; return nil, true, nil })
	return removed && err == nil, err
}
func (b *Backend) Increment(ctx context.Context, key cache.EntryKey, delta int64, ttl cache.TTL) (int64, error) {
	if err := b.validate(ctx, key); err != nil {
		return 0, err
	}
	if err := ttl.Validate(); err != nil {
		return 0, err
	}
	var result int64
	err := b.access(ctx, key, func(old *Record) (*Record, bool, error) {
		var value int64
		var err error
		if old != nil {
			value, err = cacheint.Decode(old.Data)
			if err != nil {
				return nil, false, err
			}
		}
		result, err = cacheint.Add(value, delta)
		if err != nil {
			return nil, false, err
		}
		next := b.record(cacheint.Encode(result), ttl)
		if old != nil {
			next.Expires = old.Expires
		}
		return next, true, nil
	})
	if err != nil {
		return 0, err
	}
	return result, nil
}
func (b *Backend) Exists(ctx context.Context, key cache.EntryKey) (bool, error) {
	return b.inspect(ctx, key, nil)
}
func (b *Backend) Expire(ctx context.Context, key cache.EntryKey, ttl cache.TTL) (bool, error) {
	return b.inspect(ctx, key, &ttl)
}
func (b *Backend) inspect(ctx context.Context, key cache.EntryKey, ttl *cache.TTL) (bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return false, err
	}
	if ttl != nil {
		if err := ttl.Validate(); err != nil {
			return false, err
		}
	}
	return b.driver.Inspect(ctx, key, func(expires time.Time) (*time.Time, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		// Waiting for a process/database lock must not freeze liveness or
		// consume the requested relative TTL before the mutation can occur.
		now := b.clock.Now()
		if !expires.IsZero() && !now.Before(expires) {
			return nil, false, nil
		}
		if ttl == nil {
			return nil, true, nil
		}
		var next time.Time
		if !ttl.IsForever() {
			next = now.Add(ttl.Duration())
		}
		return &next, true, nil
	})
}
