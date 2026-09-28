// Package file implements a bounded persistent cache under an owned os.Root.
// Atomic operations serialize across local processes; network filesystems are
// unsupported. Tags, batch deletion and distributed fill leases are not supplied.
package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
)

const marker = ".foundry-cache"
const lockName = ".foundry-cache.lock"
const magic = "FOUNDRY-CACHE-1\n"
const MaxPrune = 256

// Config bounds physical entries and bytes (including record envelopes). Expired
// files count until explicitly pruned. Roots must be existing trusted directories.
// Every process sharing a root must use the same bounds and synchronized clocks.
type Config struct {
	Root          string
	MaxEntries    int
	MaxBytes      int64
	MaxValueBytes int
	Sync          bool
	Clock         clock.Clock
}

func DefaultConfig(root string) Config {
	limits := cacheatomic.DefaultLimits()
	return Config{Root: root, MaxEntries: limits.MaxEntries, MaxBytes: limits.MaxBytes, MaxValueBytes: limits.MaxValueBytes, Sync: true, Clock: clock.System{}}
}
func (c Config) Validate() error {
	if err := (cacheatomic.Limits{MaxEntries: c.MaxEntries, MaxBytes: c.MaxBytes, MaxValueBytes: c.MaxValueBytes}).Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(c.Root) || strings.ContainsRune(c.Root, 0) || credential.IsNil(c.Clock) || (runtime.GOOS != "darwin" && runtime.GOOS != "linux") {
		return fault.New(fault.Invalid, "invalid file cache configuration")
	}
	return nil
}

// Backend's embedded implementation supplies only the capabilities it supports.
// Close prevents new operations and waits for active filesystem work. Filesystem
// syscalls cannot be interrupted, but contention observes the caller's context.
type Backend struct {
	*cacheatomic.Backend
	config      Config
	mu          sync.Mutex
	root        *os.Root
	active      int
	closed      bool
	done        chan struct{}
	closeErr    error
	starting    bool
	startCancel context.CancelFunc
}

func Prepare(config Config) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	b := &Backend{config: config, done: make(chan struct{})}
	var err error
	b.Backend, err = cacheatomic.New(b, config.Clock, config.MaxValueBytes)
	if err != nil {
		return nil, err
	}
	return b, nil
}
func Open(ctx context.Context, config Config) (*Backend, error) {
	b, err := Prepare(config)
	if err != nil {
		return nil, err
	}
	if err = b.Start(ctx); err != nil {
		_ = b.Close(context.Background())
		return nil, err
	}
	return b, nil
}
func (b *Backend) Start(ctx context.Context) error {
	if b == nil || b.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "file cache needs a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fault.New(fault.Closed, "file cache is closed")
	}
	if b.root != nil {
		b.mu.Unlock()
		return nil
	}
	if b.starting {
		b.mu.Unlock()
		return fault.New(fault.Conflict, "file cache startup is already running")
	}
	operation, cancel := context.WithCancel(ctx)
	b.starting = true
	b.startCancel = cancel
	b.active++
	b.mu.Unlock()
	root, err := os.OpenRoot(b.config.Root)
	if err == nil {
		err = initialize(operation, root)
	}
	if err != nil && root != nil {
		err = errors.Join(err, root.Close())
		root = nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	defer cancel()
	if b.closed && root != nil {
		err = errors.Join(fault.New(fault.Closed, "file cache closed during startup"), root.Close())
		root = nil
	}
	b.root = root
	b.starting = false
	b.startCancel = nil
	b.active--
	b.finish()
	return safe(err)
}
func initialize(ctx context.Context, root *os.Root) error {
	// Do not even create a lock file in an unrelated directory.
	checkEmpty := func() error {
		dir, err := root.Open(".")
		if err != nil {
			return safe(err)
		}
		entries, err := dir.ReadDir(3)
		closeErr := dir.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return safe(err)
		}
		if closeErr != nil {
			return safe(closeErr)
		}
		for _, entry := range entries {
			if entry.Name() != lockName {
				return fault.New(fault.Invalid, "file cache root is not empty or owned")
			}
		}
		return nil
	}
	if _, err := root.Lstat(marker); errors.Is(err, fs.ErrNotExist) {
		if err = checkEmpty(); err != nil {
			return err
		}
	} else if err != nil {
		return safe(err)
	}
	release, err := filelock.Acquire(ctx, root, lockName)
	if err != nil {
		return safe(err)
	}
	defer release()
	info, err := root.Lstat(marker)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(len(magic)) {
			return fault.New(fault.Invalid, "invalid file cache marker")
		}
		f, err := root.Open(marker)
		if err != nil {
			return safe(err)
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(len(magic)+1)))
		err = errors.Join(err, f.Close())
		if err != nil {
			return safe(err)
		}
		if string(data) != magic {
			return fault.New(fault.Invalid, "invalid file cache marker")
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return safe(err)
	}
	if err = checkEmpty(); err != nil {
		return err
	}
	f, err := root.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return safe(err)
	}
	_, err = f.WriteString(magic)
	return safe(errors.Join(err, f.Sync(), f.Close()))
}
func (b *Backend) enter(ctx context.Context) (*os.Root, func(), error) {
	if b == nil || b.done == nil || ctx == nil {
		return nil, nil, fault.New(fault.Invalid, "file cache needs a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, nil, fault.New(fault.Closed, "file cache is closed")
	}
	if b.root == nil {
		return nil, nil, fault.New(fault.Invalid, "file cache has not started")
	}
	b.active++
	return b.root, func() { b.mu.Lock(); defer b.mu.Unlock(); b.active--; b.finish() }, nil
}
func (b *Backend) finish() {
	if b.closed && b.active == 0 {
		select {
		case <-b.done:
			return
		default:
		}
		if b.root != nil {
			b.closeErr = safe(b.root.Close())
			b.root = nil
		}
		close(b.done)
	}
}
func (b *Backend) Close(ctx context.Context) error {
	if b == nil || b.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "file cache close needs context")
	}
	b.mu.Lock()
	b.closed = true
	if b.startCancel != nil {
		b.startCancel()
	}
	b.finish()
	done := b.done
	b.mu.Unlock()
	select {
	case <-done:
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Backend) Done() <-chan struct{} { return b.done }
func filename(key cache.EntryKey) string {
	digest := sha256.Sum256([]byte(key.String()))
	return hex.EncodeToString(digest[:]) + ".cache"
}

func (b *Backend) Access(ctx context.Context, key cache.EntryKey, change cacheatomic.Change) error {
	root, leave, err := b.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	release, err := filelock.Acquire(ctx, root, lockName)
	if err != nil {
		return safe(err)
	}
	defer release()
	name := filename(key)
	old, err := b.read(ctx, root, name, key.String())
	if err != nil {
		return err
	}
	next, update, err := change(old)
	if err != nil || !update {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if next == nil {
		err = root.Remove(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err == nil && b.config.Sync {
			return syncRoot(root)
		}
		return safe(err)
	}
	size := int64(recordHeaderBytes + len(key.String()) + len(next.Data))
	if err = b.budget(ctx, root, name, size); err != nil {
		return err
	}
	return b.publish(ctx, root, name, key.String(), next.Expires, int64(len(next.Data)), bytes.NewReader(next.Data))
}

func (b *Backend) budget(ctx context.Context, root *os.Root, replace string, size int64) error {
	dir, err := root.Open(".")
	if err != nil {
		return safe(err)
	}
	defer dir.Close()
	count, total := 0, size
	scanned := 0
	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			scanned++
			if scanned > b.config.MaxEntries+MaxPrune+3 {
				return fault.New(fault.Invalid, "file cache scan limit reached; prune required")
			}
			name := entry.Name()
			if name == marker || name == lockName {
				continue
			}
			pending := strings.HasPrefix(name, ".pending-")
			if (!pending && (len(name) != 70 || !strings.HasSuffix(name, ".cache"))) || !entry.Type().IsRegular() {
				return fault.New(fault.Invalid, "unrecognized file in cache root")
			}
			if name == replace {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return safe(err)
			}
			if !pending {
				count++
			}
			if info.Size() > b.config.MaxBytes-total {
				return fault.New(fault.Invalid, "file cache capacity reached; prune required")
			}
			total += info.Size()
			if count >= b.config.MaxEntries || total > b.config.MaxBytes {
				return fault.New(fault.Invalid, "file cache capacity reached; prune required")
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return safe(readErr)
		}
	}
	if total > b.config.MaxBytes {
		return fault.New(fault.Invalid, "file cache capacity reached")
	}
	return nil
}

// Prune scans at most MaxEntries+MaxPrune+3 entries and deletes at most limit
// expired records/orphan publications. A live writer holds the same process lock.
func (b *Backend) Prune(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > MaxPrune {
		return 0, fault.New(fault.Invalid, "invalid cache prune limit")
	}
	root, leave, err := b.enter(ctx)
	if err != nil {
		return 0, err
	}
	defer leave()
	release, err := filelock.Acquire(ctx, root, lockName)
	if err != nil {
		return 0, safe(err)
	}
	defer release()
	dir, err := root.Open(".")
	if err != nil {
		return 0, safe(err)
	}
	defer dir.Close()
	removed, scanned := 0, 0
	for removed < limit && scanned < b.config.MaxEntries+MaxPrune+3 {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return removed, err
			}
			scanned++
			if removed == limit || scanned > b.config.MaxEntries+MaxPrune+3 {
				return removed, nil
			}
			name := entry.Name()
			remove := strings.HasPrefix(name, ".pending-") && entry.Type().IsRegular()
			if len(name) == 70 && strings.HasSuffix(name, ".cache") {
				record, err := b.openRecord(root, name, "")
				if err != nil {
					return removed, err
				}
				if record != nil {
					err = errors.Join(record.verify(ctx, io.Discard), record.file.Close())
					if err != nil {
						return removed, err
					}
					remove = !record.expires.IsZero() && !b.config.Clock.Now().Before(record.expires)
				}
			}
			if remove {
				if err := root.Remove(name); err != nil {
					return removed, safe(err)
				}
				removed++
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return removed, safe(readErr)
		}
	}
	return removed, nil
}
func safe(err error) error {
	if err == nil {
		return nil
	}
	return fault.Wrap(fault.Invalid, "file cache operation failed", err)
}
