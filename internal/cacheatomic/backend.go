// Package cacheatomic shares expiry, counter and mutation semantics for durable
// cache adapters. The driver serializes Access and Inspect across processes for
// the same key; reads observe one record without that lock.
package cacheatomic

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
)

type Record struct {
	Data    []byte
	Expires time.Time
}

func (r *Record) Live(now time.Time) bool {
	return r != nil && (r.Expires.IsZero() || now.Before(r.Expires))
}

// Mode selects how much of an existing record a mutation needs.
type Mode uint8

const (
	// Metadata supplies only the previous record's expiry. Replacement needs
	// nothing else, so drivers skip reading and verifying the old payload.
	Metadata Mode = iota
	// Payload supplies the previous record's verified bytes.
	Payload
)

// Change runs while the driver owns the key's atomic lock. now is the storage
// authority's time, read after the lock was acquired. previous is nil when the
// entry is absent or unusable (corrupt, or larger than the current bound); such
// an entry is still replaced or removed by a write. update=false retains bytes;
// update=true with nil next deletes. Errors preserve the previous entry.
// Drivers may run a Change more than once before one attempt succeeds.
type Change func(now time.Time, previous *Record) (next *Record, update bool, err error)

// Inspection runs after the driver acquires its lock and reads the record.
// It checks current liveness at now and optionally returns a replacement expiry.
// A nil replacement preserves metadata; false never revives an expired entry.
type Inspection func(now, expires time.Time) (replacement *time.Time, live bool, err error)

// Driver owns storage, locking and the authority clock. Read observes one
// record without the mutation lock and returns the authority time of that
// observation; unusable records read as nil. data=false validates the stored
// record without returning its bytes.
type Driver interface {
	Read(ctx context.Context, key cache.EntryKey, data bool) (*Record, time.Time, error)
	Access(ctx context.Context, key cache.EntryKey, mode Mode, change Change) error
	Inspect(ctx context.Context, key cache.EntryKey, inspection Inspection) (bool, error)
}
type Backend struct {
	driver   Driver
	maxValue int
}

func New(driver Driver, maxValue int) (*Backend, error) {
	if driver == nil || !ValidValueLimit(maxValue) {
		return nil, fault.New(fault.Invalid, "invalid persistent cache configuration")
	}
	return &Backend{driver: driver, maxValue: maxValue}, nil
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
func (b *Backend) access(ctx context.Context, key cache.EntryKey, mode Mode, fn func(now time.Time, previous *Record) (*Record, bool, error)) error {
	if err := b.validate(ctx, key); err != nil {
		return err
	}
	return b.driver.Access(ctx, key, mode, func(now time.Time, old *Record) (*Record, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		// An over-bound record written under an earlier, larger limit is a miss
		// that this operation may replace or remove.
		if old != nil && len(old.Data) > b.maxValue || !old.Live(now) {
			old = nil
		}
		next, update, err := fn(now, old)
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
func record(now time.Time, data []byte, ttl cache.TTL) *Record {
	r := &Record{Data: slices.Clone(data)}
	if !ttl.IsForever() {
		r.Expires = now.Add(ttl.Duration())
	}
	return r
}

// read observes a live, usable record without taking the mutation lock.
func (b *Backend) read(ctx context.Context, key cache.EntryKey, data bool) (*Record, error) {
	if err := b.validate(ctx, key); err != nil {
		return nil, err
	}
	current, now, err := b.driver.Read(ctx, key, data)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !current.Live(now) || data && len(current.Data) > b.maxValue {
		return nil, nil
	}
	return current, nil
}
func (b *Backend) Get(ctx context.Context, key cache.EntryKey) ([]byte, bool, error) {
	current, err := b.read(ctx, key, true)
	if err != nil {
		return nil, false, err
	}
	if current == nil {
		return nil, false, nil
	}
	data := current.Data
	if data == nil {
		data = []byte{}
	}
	return data, true, nil
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
	mode := Metadata
	if absent {
		// Add must agree with Get about corrupt payloads, so it verifies bytes.
		mode = Payload
	}
	changed := false
	err := b.access(ctx, key, mode, func(now time.Time, old *Record) (*Record, bool, error) {
		if absent && old != nil {
			changed = false
			return nil, false, nil
		}
		changed = true
		return record(now, data, ttl), true, nil
	})
	return changed && err == nil, err
}
func (b *Backend) Forget(ctx context.Context, key cache.EntryKey) (bool, error) {
	removed := false
	err := b.access(ctx, key, Payload, func(_ time.Time, old *Record) (*Record, bool, error) {
		removed = old != nil
		return nil, true, nil
	})
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
	err := b.access(ctx, key, Payload, func(now time.Time, old *Record) (*Record, bool, error) {
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
		next := record(now, cacheint.Encode(result), ttl)
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

// Exists validates the stored record without the mutation lock. A corrupt or
// over-bound record is absent. The observation is not a reservation.
func (b *Backend) Exists(ctx context.Context, key cache.EntryKey) (bool, error) {
	current, err := b.read(ctx, key, false)
	return current != nil && err == nil, err
}
func (b *Backend) Expire(ctx context.Context, key cache.EntryKey, ttl cache.TTL) (bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return false, err
	}
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	return b.driver.Inspect(ctx, key, func(now, expires time.Time) (*time.Time, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		// now was read after the lock, so waiting for a process/database lock
		// never freezes liveness or consumes part of the requested relative TTL.
		if !expires.IsZero() && !now.Before(expires) {
			return nil, false, nil
		}
		var next time.Time
		if !ttl.IsForever() {
			next = now.Add(ttl.Duration())
		}
		return &next, true, nil
	})
}
