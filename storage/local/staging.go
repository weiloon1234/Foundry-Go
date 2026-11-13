package local

import (
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// StagingLimits reserves large-write capacity inside the managed root. Zero
// disables this additional limit. Known sizes at most SmallObjectBytes bypass
// the large pool; unknown sizes reserve MaxObjectBytes. MaxBytes bounds payload
// reservations, including failed cleanup. Provision record/filesystem overhead
// and retained published data separately, and reserve small-disk capacity.
type StagingLimits struct {
	MaxActive        int
	MaxBytes         int64
	SmallObjectBytes int64
}

func (s StagingLimits) validate(maximum int64) error {
	if s == (StagingLimits{}) {
		return nil
	}
	if s.MaxActive < 1 || s.MaxActive > 4096 || s.MaxBytes < maximum || s.SmallObjectBytes < 0 || s.SmallObjectBytes >= maximum {
		return failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
	}
	return nil
}

type stagingPool struct {
	mu       sync.Mutex
	cleanup  sync.Mutex
	slots    *admission.Semaphore
	bytes    int64
	retained map[string]int64
}

// Startup accounts for interrupted writers before admitting new ones. Existing
// cleanup still owns recognition, writer locks, deletion and durability.
func (b *Backend) startStaging(ctx context.Context) error {
	if b.config.Staging.MaxActive == 0 {
		return nil
	}
	b.staging = &stagingPool{slots: admission.New(b.config.Staging.MaxActive), retained: make(map[string]int64)}
	return b.walk(ctx, func(parent *os.Root, name string) error {
		if !stagingName(name) {
			return nil
		}
		info, err := parent.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fs.ErrInvalid
		}
		// Counting the entire interrupted record is conservative and includes its
		// framing. A fresh operation reserves its payload maximum before writing.
		if info.Size() > math.MaxInt64-b.staging.bytes {
			return failure(storage.LimitExceeded, storage.OpenOperation, storage.Unchanged, nil)
		}
		b.staging.retained[name] = info.Size()
		b.staging.bytes += info.Size()
		return nil
	})
}

func (b *Backend) reserveStaging(options storage.PutOptions, op storage.Operation) (func(string), error) {
	maximum := b.config.MaxObjectBytes
	if size, known := options.Size.Get(); known {
		maximum = size
	}
	if b.staging == nil || maximum <= b.config.Staging.SmallObjectBytes {
		return func(string) {}, nil
	}
	pool := b.staging
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if maximum > b.config.Staging.MaxBytes-pool.bytes || !pool.slots.TryAcquire() {
		return nil, failure(storage.Unavailable, op, storage.Unchanged, safeerror.NewFault(fault.Overloaded, "local staging capacity is exhausted"))
	}
	pool.bytes += maximum
	var once sync.Once
	return func(retained string) {
		once.Do(func() {
			pool.mu.Lock()
			defer pool.mu.Unlock()
			if retained == "" {
				pool.bytes -= maximum
			} else {
				pool.retained[retained] = maximum
			}
			pool.slots.Release()
		})
	}, nil
}

func (b *Backend) releasedStaging(name string) {
	if b.staging == nil {
		return
	}
	b.staging.mu.Lock()
	defer b.staging.mu.Unlock()
	b.staging.bytes -= b.staging.retained[name]
	delete(b.staging.retained, name)
}

func stagingName(name string) bool {
	suffix, ok := strings.CutPrefix(name, tempPrefix)
	if !ok || len(suffix) != 32 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}
