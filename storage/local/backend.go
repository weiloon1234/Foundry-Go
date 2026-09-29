// Package local implements a managed, crash-aware object store under an os.Root.
// Keys are independent of host filenames. Each published file atomically holds
// one binary header, complete metadata and payload; temporary files are hidden.
package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"github.com/weiloon1234/Foundry-Go/storage"
)

const markerName = ".foundry-store"
const lockName = ".foundry-lock"
const lockDirectory = "locks"
const markerMagic = "FNDLOCAL1\n"
const tempPrefix = ".foundry-tmp-"
const MaxPrune = 256

type Config struct {
	Root           string
	MaxObjectBytes int64
	// MaxScan bounds files examined by one lexical listing or cleanup call. No
	// incomplete listing is returned when this budget is exhausted.
	MaxScan int
	Sync    bool
	Clock   clock.Clock
}

func DefaultConfig(root string) Config {
	return Config{Root: root, MaxObjectBytes: 1 << 30, MaxScan: 100000, Sync: true, Clock: clock.System{}}
}
func (c Config) Validate() error {
	if !filepath.IsAbs(c.Root) || strings.ContainsRune(c.Root, 0) || c.MaxObjectBytes <= 0 || c.MaxObjectBytes > 1<<50 || c.MaxScan < 1 || c.Clock == nil {
		return failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return failure(storage.Unsupported, storage.OpenOperation, storage.Unchanged, nil)
	}
	return nil
}

type Backend struct {
	root      *os.Root
	config    Config
	storeID   string
	lifecycle sync.Mutex
	prepared  bool
	started   bool
	closed    bool
	closeErr  error
	// ensured records shard directories whose creation this process has made
	// durable, so later publications skip parent directory syncs.
	ensured [256]atomic.Bool
	// beforeSync, when set by package tests, observes the moment before a
	// publication's or removal's directory sync. It is nil in production.
	beforeSync func(storage.ObjectKey)
}

// Prepare validates configuration without filesystem I/O. Start initializes or
// reopens the store during application boot; a closed backend cannot restart.
func Prepare(config Config) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{config: config, prepared: true}, nil
}

// Start takes ownership of an existing empty directory or reopens a previously
// initialized Foundry store. It never imports, wipes or overwrites an unrelated
// directory. The root must be controlled by trusted operators, not upload users.
// New files/directories use 0600/0700. Close all Disk operations before Close.
func (b *Backend) Start(ctx context.Context) error {
	if b == nil || ctx == nil {
		return failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if !b.prepared {
		return failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
	}
	if b.closed {
		return failure(storage.Closed, storage.OpenOperation, storage.Unchanged, nil)
	}
	if err := ctx.Err(); err != nil {
		return failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, err)
	}
	if b.started {
		return nil
	}
	opened, err := initialize(ctx, b.config)
	if err != nil {
		return err
	}
	b.root, b.storeID, b.started = opened.root, opened.storeID, true
	return nil
}

// Open is Prepare followed by Start for callers that explicitly own the adapter.
func Open(ctx context.Context, config Config) (*Backend, error) {
	backend, err := Prepare(config)
	if err != nil {
		return nil, err
	}
	if err = backend.Start(ctx); err != nil {
		return nil, err
	}
	return backend, nil
}

func initialize(ctx context.Context, config Config) (result *Backend, err error) {
	if ctx == nil {
		return nil, failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
	}
	if err = config.Validate(); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, err)
	}
	root, err := os.OpenRoot(config.Root)
	if err != nil {
		return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, err)
	}
	b := &Backend{root: root, config: config}
	defer func() {
		if err != nil {
			err = errors.Join(err, failureIf(root.Close(), storage.CloseOperation, storage.NotApplicable))
		}
	}()
	// Reject unrelated directories before creating even the control lock file.
	if _, e := root.Lstat(markerName); errors.Is(e, fs.ErrNotExist) {
		directory, e := root.Open(".")
		if e != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
		}
		entries, readErr := directory.ReadDir(3)
		closeErr := directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, errors.Join(readErr, closeErr))
		}
		for _, entry := range entries {
			if entry.Name() != lockName {
				return nil, failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
			}
		}
	} else if e != nil {
		return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
	}
	unlock, err := b.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	marker, openErr := root.Open(markerName)
	if openErr == nil {
		defer marker.Close()
		info, e := marker.Stat()
		if e != nil || !info.Mode().IsRegular() {
			return nil, failure(storage.IntegrityFailed, storage.OpenOperation, storage.Unchanged, e)
		}
		data, e := io.ReadAll(io.LimitReader(marker, 128))
		if e != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
		}
		if len(data) != len(markerMagic)+32 || !strings.HasPrefix(string(data), markerMagic) {
			return nil, failure(storage.IntegrityFailed, storage.OpenOperation, storage.Unchanged, nil)
		}
		b.storeID = string(data[len(markerMagic):])
		decoded, e := hex.DecodeString(b.storeID)
		if e != nil || len(decoded) != 16 {
			return nil, failure(storage.IntegrityFailed, storage.OpenOperation, storage.Unchanged, e)
		}
	} else {
		if !errors.Is(openErr, fs.ErrNotExist) {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, openErr)
		}
		dir, e := root.Open(".")
		if e != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
		}
		entries, readErr := dir.ReadDir(3)
		closeErr := dir.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, errors.Join(readErr, closeErr))
		}
		for _, entry := range entries {
			if entry.Name() != lockName {
				return nil, failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
			}
		}
		id := make([]byte, 16)
		if _, e = rand.Read(id); e != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
		}
		b.storeID = hex.EncodeToString(id)
		file, e := root.OpenFile(markerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
		}
		_, e = file.WriteString(markerMagic + b.storeID)
		if e == nil && config.Sync {
			e = file.Sync()
		}
		e = errors.Join(e, file.Close())
		if e != nil {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unknown, e)
		}
		if config.Sync {
			if e = syncDirectory(root); e != nil {
				return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Applied, e)
			}
		}
	}
	// Per-key publication locks live in their own directory, created once
	// under the exclusive store lock.
	if _, e := root.Lstat(lockDirectory); errors.Is(e, fs.ErrNotExist) {
		if e = root.Mkdir(lockDirectory, 0700); e != nil && !errors.Is(e, fs.ErrExist) {
			return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
		}
		if config.Sync {
			if e = syncDirectory(root); e != nil {
				return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Applied, e)
			}
		}
	} else if e != nil {
		return nil, failure(storage.Unavailable, storage.OpenOperation, storage.Unchanged, e)
	}
	if err = ctx.Err(); err != nil {
		return nil, failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, err)
	}
	return b, nil
}
func (b *Backend) Capabilities() storage.Capabilities {
	return storage.Capabilities{Ranges: true, ConditionalRead: true, ConditionalCreate: true, ConditionalReplace: true, ConditionalDelete: true, DelimitedList: true}
}
func (b *Backend) Close() error {
	if b == nil {
		return nil
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if b.closed {
		return b.closeErr
	}
	b.closed = true
	if b.root != nil {
		b.closeErr = failureIf(b.root.Close(), storage.CloseOperation, storage.NotApplicable)
	}
	return b.closeErr
}
func (b *Backend) ready(ctx context.Context, op storage.Operation) error {
	if b == nil || ctx == nil {
		return failure(storage.Invalid, op, storage.NotApplicable, nil)
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if !b.prepared || !b.started {
		return failure(storage.Invalid, op, storage.NotApplicable, nil)
	}
	if b.closed {
		return failure(storage.Closed, op, storage.NotApplicable, nil)
	}
	if err := ctx.Err(); err != nil {
		return failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	return nil
}
func failure(code storage.Code, op storage.Operation, state storage.Outcome, err error) *storage.Error {
	var existing *storage.Error
	var permission, missing, closed bool
	complete := errorgraph.Walk(err, func(current error) bool {
		if detail, matched := errorgraph.AsShallow[*storage.Error](current); matched {
			existing = detail
			return false
		}
		permission = permission || errorgraph.Matches(current, fs.ErrPermission)
		missing = missing || errorgraph.Matches(current, fs.ErrNotExist)
		closed = closed || errorgraph.Matches(current, fs.ErrClosed)
		return true
	})
	if existing != nil {
		code = existing.Code()
	} else if complete {
		switch {
		case permission:
			code = storage.Forbidden
		case missing:
			code = storage.NotFound
		case closed:
			code = storage.Closed
		}
	}
	return storage.Failure(code, op, state, err)
}
func failureIf(err error, op storage.Operation, state storage.Outcome) error {
	if err == nil {
		return nil
	}
	return failure(storage.Unavailable, op, state, err)
}
func syncDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

// Each acquisition has a separate open-file description, so flock serializes
// goroutines and independent processes, not just different adapter instances.
// The exclusive store lock guards initialization.
func (b *Backend) lock(ctx context.Context) (func(), error) {
	return lockFailure(filelock.Acquire(ctx, b.root, lockName))
}

// lockKey serializes check-and-publish for one key's hash shard. Publications
// hold the store lock shared, so earlier framework versions that take it
// exclusively still exclude every writer, while distinct shards proceed in
// parallel. Hold it only across the condition check and rename.
func (b *Backend) lockKey(ctx context.Context, key storage.ObjectKey) (func(), error) {
	store, err := lockFailure(filelock.AcquireShared(ctx, b.root, lockName))
	if err != nil {
		return nil, err
	}
	_, name := address(key)
	shard, err := lockFailure(filelock.Acquire(ctx, b.root, lockDirectory+"/"+name[:2]))
	if err != nil {
		store()
		return nil, err
	}
	return func() { shard(); store() }, nil
}
func lockFailure(release func(), err error) (func(), error) {
	if errors.Is(err, fault.Invalid) {
		return nil, failure(storage.IntegrityFailed, storage.PutOperation, storage.Unchanged, err)
	}
	if err != nil {
		return nil, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	return release, nil
}

// ensureShard creates a key's shard directory. The first publication to each
// shard in this process syncs its parents; later ones skip both syncs.
func (b *Backend) ensureShard(directory string, index byte) error {
	if b.ensured[index].Load() {
		return nil
	}
	if err := b.root.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if b.config.Sync {
		objects, err := b.root.OpenRoot("objects")
		if err != nil {
			return err
		}
		if err = errors.Join(syncDirectory(objects), objects.Close(), syncDirectory(b.root)); err != nil {
			return err
		}
	}
	b.ensured[index].Store(true)
	return nil
}

var _ storage.Backend = (*Backend)(nil)
