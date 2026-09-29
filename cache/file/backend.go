// Package file implements a bounded persistent cache under an owned os.Root.
// Atomic operations serialize across local processes per key shard; network
// filesystems are unsupported. Tags, batch deletion and distributed fill leases
// are not supplied. Store.Invalidate physically removes a namespace's records.
package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
)

const marker = ".foundry-cache"

// lockName serializes root initialization only. Entry operations use one of
// lockShards shard locks selected by the record's address digest.
const lockName = ".foundry-cache.lock"
const shardLockPrefix = lockName + "."
const lockShards = 32
const pendingPrefix = ".pending-"
const magic = "FOUNDRY-CACHE-1\n"
const MaxPrune = cacheatomic.MaxPrune

// PruneResult reports one bounded prune pass, including skipped corrupt records.
type PruneResult = cacheatomic.PruneResult

// Config bounds physical record files and bytes (including record envelopes).
// A write never fails because of expired records: at capacity the backend first
// reclaims up to MaxPrune expired records, then rejects only if still full.
// Usage is an estimate maintained by this process and recounted by every prune
// pass, so processes sharing a root can briefly overshoot between passes.
// Roots must be existing trusted directories. Every process sharing a root must
// use the same bounds, synchronized clocks and the same Foundry version.
type Config struct {
	Root          string
	MaxEntries    int
	MaxBytes      int64
	MaxValueBytes int
	Sync          bool
	Clock         clock.Clock
	// PruneInterval runs automatic reclamation of expired records while the
	// backend is started. Zero disables it; otherwise 1s to 24h.
	PruneInterval time.Duration
	// Logger optionally receives redacted automatic-prune failures.
	Logger *slog.Logger
}

func DefaultConfig(root string) Config {
	limits := cacheatomic.DefaultLimits()
	return Config{Root: root, MaxEntries: limits.MaxEntries, MaxBytes: limits.MaxBytes, MaxValueBytes: limits.MaxValueBytes, Sync: true, Clock: clock.System{}, PruneInterval: cacheatomic.DefaultPruneInterval}
}
func (c Config) Validate() error {
	if err := c.limits().Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(c.Root) || strings.ContainsRune(c.Root, 0) || credential.IsNil(c.Clock) || !cacheatomic.ValidPruneInterval(c.PruneInterval) || (runtime.GOOS != "darwin" && runtime.GOOS != "linux") {
		return fault.New(fault.Invalid, "invalid file cache configuration")
	}
	return nil
}
func (c Config) limits() cacheatomic.Limits {
	return cacheatomic.Limits{MaxEntries: c.MaxEntries, MaxBytes: c.MaxBytes, MaxValueBytes: c.MaxValueBytes}
}

// Backend's embedded implementation supplies only the capabilities it supports.
// Close prevents new operations, stops the pruner and waits for active
// filesystem work. Filesystem syscalls cannot be interrupted, but contention
// observes the caller's context. Reads take no lock: records are published by
// atomic rename, so a reader sees either the old or the new complete record.
type Backend struct {
	*cacheatomic.Backend
	config      Config
	usage       cacheatomic.Usage
	shards      [lockShards]chan struct{}
	mu          sync.Mutex
	root        *os.Root
	active      int
	closed      bool
	done        chan struct{}
	closeErr    error
	starting    bool
	startCancel context.CancelFunc
	stopPruner  context.CancelFunc
}

var _ cache.FlushBackend = (*Backend)(nil)

// FoundryAdapter marks the backend as framework-owned adapter I/O.
func (*Backend) FoundryAdapter(frameworkadapter.Seal) {}

func Prepare(config Config) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	b := &Backend{config: config, done: make(chan struct{})}
	for i := range b.shards {
		b.shards[i] = make(chan struct{}, 1)
	}
	var err error
	b.Backend, err = cacheatomic.New(b, config.MaxValueBytes)
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

// Start opens and initializes the root, counts its records and, when
// PruneInterval is positive, starts the owned background pruner.
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
	if err == nil {
		_, err = b.sweep(operation, root, 0, "")
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
	if root != nil && b.config.PruneInterval > 0 {
		lifetime, stop := context.WithCancel(context.Background())
		b.stopPruner = stop
		b.active++
		go func() {
			defer b.leave()
			cacheatomic.RunPruner(lifetime, b.config.PruneInterval, b.config.Logger, "file", func(ctx context.Context) (PruneResult, error) {
				return b.Sweep(ctx, MaxPrune)
			})
		}()
	}
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
		return createShardLocks(root)
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
	if err = safe(errors.Join(err, f.Sync(), f.Close())); err != nil {
		return err
	}
	return createShardLocks(root)
}

// createShardLocks creates every shard lock file under the initialization lock.
// Entry operations then only open existing lock files: concurrent O_CREAT of one
// new file by several processes can fail spuriously on some filesystems.
func createShardLocks(root *os.Root) error {
	for shard := range lockShards {
		f, err := root.OpenFile(shardLock(shard), os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return safe(err)
		}
		if err = f.Close(); err != nil {
			return safe(err)
		}
	}
	return nil
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
	return b.root, b.leave, nil
}
func (b *Backend) leave() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active--
	b.finish()
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
	if b.stopPruner != nil {
		b.stopPruner()
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

// shardOf maps a record file name to its lock shard from the address digest.
func shardOf(name string) int {
	value, err := strconv.ParseUint(name[:2], 16, 8)
	if err != nil {
		return 0
	}
	return int(value) % lockShards
}
func isRecord(name string) bool {
	if len(name) != 2*sha256.Size+len(".cache") || !strings.HasSuffix(name, ".cache") {
		return false
	}
	_, err := hex.DecodeString(name[:2*sha256.Size])
	return err == nil
}
func shardLock(shard int) string { return shardLockPrefix + hex.EncodeToString([]byte{byte(shard)}) }
func isShardLock(name string) bool {
	suffix, ok := strings.CutPrefix(name, shardLockPrefix)
	if !ok || len(suffix) != 2 {
		return false
	}
	value, err := strconv.ParseUint(suffix, 16, 8)
	return err == nil && value < lockShards
}

// pendingShard recovers the shard of an in-flight publication. Legacy names
// without a shard belong to shard zero; mixing versions on one root is unsupported.
func pendingShard(name string) int {
	rest := strings.TrimPrefix(name, pendingPrefix)
	if len(rest) > 3 && rest[2] == '-' {
		if value, err := strconv.ParseUint(rest[:2], 16, 8); err == nil && value < lockShards {
			return int(value)
		}
	}
	return 0
}

// lockShard serializes one address shard: an in-process slot first, so local
// goroutines queue without polling, then the shard's file lock across processes.
func (b *Backend) lockShard(ctx context.Context, root *os.Root, shard int) (func(), error) {
	select {
	case b.shards[shard] <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release, err := filelock.Acquire(ctx, root, shardLock(shard))
	if err != nil {
		<-b.shards[shard]
		return nil, safe(err)
	}
	return func() { release(); <-b.shards[shard] }, nil
}

// Read observes one record without a lock. Corrupt and over-bound records are
// misses. The returned time is the configured clock after the read.
func (b *Backend) Read(ctx context.Context, key cache.EntryKey, data bool) (*cacheatomic.Record, time.Time, error) {
	root, leave, err := b.enter(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer leave()
	current, err := b.load(ctx, root, filename(key), key.String(), cacheatomic.Payload, data)
	if err != nil {
		return nil, time.Time{}, err
	}
	return current, b.config.Clock.Now(), nil
}

// Access runs one atomic change under the key's shard lock. A write that does
// not fit first reclaims expired records (at most once per second per backend)
// and then retries; only a cache still full of live records rejects it.
func (b *Backend) Access(ctx context.Context, key cache.EntryKey, mode cacheatomic.Mode, change cacheatomic.Change) error {
	root, leave, err := b.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	name := filename(key)
	shard := shardOf(name)
	for reclaimed := false; ; reclaimed = true {
		unlock, err := b.lockShard(ctx, root, shard)
		if err != nil {
			return err
		}
		full, err := b.accessLocked(ctx, root, name, key.String(), shard, mode, change)
		unlock()
		if err != nil || !full {
			return err
		}
		if reclaimed || !b.usage.ReconcileDue() {
			return cacheatomic.CapacityError("file cache")
		}
		result, err := b.sweep(ctx, root, MaxPrune, "")
		if err != nil {
			return err
		}
		b.usage.Reclaimed(result)
	}
}
func (b *Backend) accessLocked(ctx context.Context, root *os.Root, name, key string, shard int, mode cacheatomic.Mode, change cacheatomic.Change) (bool, error) {
	previous := int64(-1)
	info, err := root.Lstat(name)
	switch {
	case err == nil && info.Mode().IsRegular():
		previous = info.Size()
	case err == nil:
		return false, fault.New(fault.Invalid, "invalid file cache record")
	case !errors.Is(err, fs.ErrNotExist):
		return false, safe(err)
	}
	var old *cacheatomic.Record
	if previous >= 0 {
		if old, err = b.load(ctx, root, name, key, mode, true); err != nil {
			return false, err
		}
	}
	next, update, err := change(b.config.Clock.Now(), old)
	if err != nil || !update {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if next == nil {
		if previous < 0 {
			return false, nil
		}
		if err = root.Remove(name); errors.Is(err, fs.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, safe(err)
		}
		b.usage.Add(-1, -previous)
		if b.config.Sync {
			return false, syncRoot(root)
		}
		return false, nil
	}
	size := int64(recordHeaderBytes + len(key) + len(next.Data))
	if !b.usage.Fits(b.config.limits(), previous, size) {
		return true, nil
	}
	if err = b.publish(ctx, root, name, key, shard, next.Expires, int64(len(next.Data)), bytes.NewReader(next.Data)); err != nil {
		return false, err
	}
	if previous < 0 {
		b.usage.Add(1, size)
	} else {
		b.usage.Add(0, size-previous)
	}
	return false, nil
}

// Prune deletes at most limit expired records and orphan publications.
func (b *Backend) Prune(ctx context.Context, limit int) (int, error) {
	result, err := b.Sweep(ctx, limit)
	return result.Removed, err
}

// Sweep deletes at most limit expired records and orphan publications, skipping
// (and counting) corrupt records and foreign files instead of stopping. It
// locks one shard at a time, never the whole root. A complete pass recounts the
// usage estimate. The scan is bounded by twice MaxEntries plus MaxPrune files.
func (b *Backend) Sweep(ctx context.Context, limit int) (PruneResult, error) {
	if limit < 1 || limit > MaxPrune {
		return PruneResult{}, fault.New(fault.Invalid, "invalid cache prune limit")
	}
	root, leave, err := b.enter(ctx)
	if err != nil {
		return PruneResult{}, err
	}
	defer leave()
	return b.sweep(ctx, root, limit, "")
}

// FlushNamespace physically removes every readable record of namespace. It
// locks one shard at a time, so it is not a fence against concurrent writers.
func (b *Backend) FlushNamespace(ctx context.Context, namespace cache.Namespace) (uint64, error) {
	prefix, err := cacheatomic.NamespacePrefix(namespace)
	if err != nil {
		return 0, err
	}
	root, leave, err := b.enter(ctx)
	if err != nil {
		return 0, err
	}
	defer leave()
	result, err := b.sweep(ctx, root, 0, prefix)
	return uint64(result.Removed), err
}

// sweep scans the root once. With flush empty it removes expired records and
// orphan publications up to limit; with flush set it removes every readable
// record whose address starts with flush. Both count what remains.
func (b *Backend) sweep(ctx context.Context, root *os.Root, limit int, flush string) (PruneResult, error) {
	var result PruneResult
	names, complete, err := b.list(ctx, root)
	if err != nil {
		return result, err
	}
	var groups [lockShards][]string
	for _, name := range names {
		switch {
		case name == marker || name == lockName || isShardLock(name):
		case strings.HasPrefix(name, pendingPrefix):
			groups[pendingShard(name)] = append(groups[pendingShard(name)], name)
		case isRecord(name):
			groups[shardOf(name)] = append(groups[shardOf(name)], name)
		default:
			result.Unrecognized++
		}
	}
	for shard, group := range groups {
		if len(group) == 0 {
			continue
		}
		if err := b.sweepShard(ctx, root, shard, group, limit, flush, &result); err != nil {
			return result, err
		}
	}
	// An incomplete pass is a lower bound near or above capacity, so later
	// writes keep reclaiming until a pass sees the whole root.
	result.Complete = complete
	b.usage.Reconcile(result.Entries, result.Bytes)
	return result, nil
}
func (b *Backend) sweepShard(ctx context.Context, root *os.Root, shard int, names []string, limit int, flush string, result *PruneResult) error {
	unlock, err := b.lockShard(ctx, root, shard)
	if err != nil {
		return err
	}
	defer unlock()
	now := b.config.Clock.Now()
	removed := false
	remove := func(name string) error {
		if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return safe(err)
		}
		result.Removed++
		removed = true
		return nil
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(name, pendingPrefix) {
			// Publication renames its pending file while holding this lock,
			// so any pending file seen here is an orphan.
			if flush == "" && result.Removed < limit {
				if err := remove(name); err != nil {
					return err
				}
			}
			continue
		}
		info, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return safe(err)
		}
		r, err := b.openRecord(root, name, "")
		if errors.Is(err, errCorrupt) {
			result.Corrupt++
			result.Entries++
			result.Bytes += info.Size()
			continue
		}
		if err != nil {
			return err
		}
		if r == nil {
			continue
		}
		_ = r.file.Close()
		drop := strings.HasPrefix(r.address, flush) && flush != "" ||
			flush == "" && result.Removed < limit && !r.expires.IsZero() && !now.Before(r.expires)
		if drop {
			if err := remove(name); err != nil {
				return err
			}
			continue
		}
		result.Entries++
		result.Bytes += info.Size()
	}
	if removed && b.config.Sync {
		return syncRoot(root)
	}
	return nil
}

// list returns at most a bounded number of directory names; complete is false
// when the root holds more files than one pass may inspect.
func (b *Backend) list(ctx context.Context, root *os.Root) ([]string, bool, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, false, safe(err)
	}
	defer dir.Close()
	bound := 2*b.config.MaxEntries + MaxPrune + lockShards + 3
	var names []string
	for len(names) <= bound {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		if errors.Is(readErr, io.EOF) {
			return names, len(names) <= bound, nil
		}
		if readErr != nil {
			return nil, false, safe(readErr)
		}
	}
	return names[:bound], false, nil
}
func safe(err error) error {
	if err == nil {
		return nil
	}
	return fault.Wrap(fault.Invalid, "file cache operation failed", err)
}
