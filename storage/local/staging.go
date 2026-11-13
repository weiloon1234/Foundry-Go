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
	retained map[string]*stagingFile
}

// stagingFile binds an accounting entry to its complete confined path and inode.
// A live foreign writer owns its reservation; its observed entry has zero bytes
// until the writer exits without removing/publishing the staging file.
type stagingFile struct {
	path  string
	info  os.FileInfo
	bytes int64
}

func (b *Backend) startStaging(ctx context.Context) error {
	if b.config.Staging.MaxActive == 0 {
		return nil
	}
	b.staging = &stagingPool{slots: admission.New(b.config.Staging.MaxActive), retained: make(map[string]*stagingFile)}
	return b.walkEntries(ctx, 0, func(_ *os.Root, directory, name string) error {
		if !stagingName(name) {
			return nil
		}
		path := directory + "/" + name
		entry, err := b.inspectStaging(path, nil)
		if err != nil {
			return err
		}
		return b.staging.record(path, entry)
	})
}

// inspectStaging never deletes a file. The descriptor pins the inode while its
// writer lock and current directory identity are checked. A concurrent rename
// cannot make a replacement inherit the previous inode's reservation.
func (b *Backend) inspectStaging(path string, previous *stagingFile) (entry *stagingFile, err error) {
	file, err := b.root.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = safeerror.Join("staging inspection failed", err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	locked, err := tryLock(file)
	if err != nil {
		return nil, err
	}
	current, err := b.root.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, current) {
		return nil, fs.ErrInvalid
	}
	same := previous != nil && (previous.info == nil || os.SameFile(previous.info, info))
	entry = &stagingFile{path: path, info: info}
	if !locked {
		// Do not duplicate a live writer's charge. Previously retained bytes stay
		// charged while another cleanup owner holds the same inode's lock.
		if same {
			entry.bytes = previous.bytes
		}
		return entry, nil
	}
	entry.bytes = info.Size()
	if same {
		entry.bytes = max(entry.bytes, previous.bytes)
	}
	return entry, nil
}

// record is called before startup completes or with the accounting mutex held.
func (p *stagingPool) record(path string, entry *stagingFile) error {
	previous := int64(0)
	if old := p.retained[path]; old != nil {
		previous = old.bytes
	}
	bytes := p.bytes - previous
	if entry != nil {
		if entry.bytes > math.MaxInt64-bytes {
			return failure(storage.LimitExceeded, storage.OpenOperation, storage.Unchanged, nil)
		}
		bytes += entry.bytes
		p.retained[path] = entry
	} else {
		delete(p.retained, path)
	}
	p.bytes = bytes
	return nil
}

// Reconcile only known recovery/cleanup identities, not the published-object
// namespace. Foreign publication, deletion, replacement and pruning all converge
// here before the next large admission or explicit cleanup attempt.
func (b *Backend) reconcileStaging(ctx context.Context) error {
	if b.staging == nil {
		return nil
	}
	pool := b.staging
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for path, previous := range pool.retained {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := b.inspectStaging(path, previous)
		if err != nil {
			return err
		}
		if err := pool.record(path, entry); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) reserveStaging(ctx context.Context, options storage.PutOptions, op storage.Operation) (func(*stagingFile), error) {
	maximum := b.config.MaxObjectBytes
	if size, known := options.Size.Get(); known {
		maximum = size
	}
	if b.staging == nil || maximum <= b.config.Staging.SmallObjectBytes {
		return func(*stagingFile) {}, nil
	}
	if err := b.reconcileStaging(ctx); err != nil {
		return nil, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	pool := b.staging
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if maximum > b.config.Staging.MaxBytes-pool.bytes || !pool.slots.TryAcquire() {
		return nil, failure(storage.Unavailable, op, storage.Unchanged, safeerror.NewFault(fault.Overloaded, "local staging capacity is exhausted"))
	}
	pool.bytes += maximum
	var once sync.Once
	return func(retained *stagingFile) {
		once.Do(func() {
			pool.mu.Lock()
			defer pool.mu.Unlock()
			if retained == nil {
				pool.bytes -= maximum
			} else {
				retained.bytes = maximum
				pool.retained[retained.path] = retained
			}
			pool.slots.Release()
		})
	}, nil
}

func (b *Backend) releasedStaging(path string, info os.FileInfo) {
	if b.staging == nil {
		return
	}
	b.staging.mu.Lock()
	defer b.staging.mu.Unlock()
	entry := b.staging.retained[path]
	if entry != nil && entry.info != nil && os.SameFile(entry.info, info) {
		b.staging.bytes -= entry.bytes
		delete(b.staging.retained, path)
	}
}

func stagingName(name string) bool {
	suffix, ok := strings.CutPrefix(name, tempPrefix)
	if !ok || len(suffix) != 32 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}
