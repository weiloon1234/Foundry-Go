package upload

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Batch owns all temporary files and readers for one request. Close waits for
// active captures/reads, closes readers, and removes only owned files. No
// recursive deletion or client-provided path participates in cleanup.
type Batch struct {
	ctx             context.Context
	config          Config
	mu              sync.Mutex
	changed         *sync.Cond
	closing, closed bool
	active, files   int
	bytes           int64
	dir             string
	root            *os.Root
	identity        fs.FileInfo
	names           map[string]struct{}
	readers         map[*Reader]struct{}
	closeErr        error
}

func New(ctx context.Context, config Config) (*Batch, error) {
	if ctx == nil {
		return nil, safeerror.NewFault(fault.Invalid, "upload batch requires a context")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := &Batch{ctx: ctx, config: config, names: make(map[string]struct{}), readers: make(map[*Reader]struct{})}
	b.changed = sync.NewCond(&b.mu)
	return b, nil
}
func (b *Batch) ready(ctx context.Context) error {
	if b.closing || b.closed {
		return fs.ErrClosed
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	return ctx.Err()
}
func (b *Batch) end() { b.mu.Lock(); b.active--; b.changed.Broadcast(); b.mu.Unlock() }
func (b *Batch) create(ctx context.Context) (*os.File, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ready(ctx); err != nil {
		return nil, "", err
	}
	if b.files >= b.config.MaxFiles {
		return nil, "", safeerror.Owned("upload resource limit exceeded", &LimitError{Kind: Files})
	}
	if b.root == nil {
		if b.dir != "" {
			return nil, "", safeerror.NewFault(fault.Conflict, "upload initialization cleanup is pending")
		}
		dir, err := os.MkdirTemp(b.config.TempDirectory, captureDirectoryPrefix)
		if err != nil {
			return nil, "", err
		}
		b.dir = dir
		root, err := os.OpenRoot(dir)
		if err != nil {
			removeErr := os.Remove(dir)
			if removeErr == nil {
				b.dir = ""
			}
			return nil, "", safeerror.Join("operation failed", err, removeErr)
		}
		identity, err := root.Stat(".")
		if err != nil {
			closeErr, removeErr := root.Close(), os.Remove(dir)
			if removeErr == nil {
				b.dir = ""
			}
			return nil, "", safeerror.Join("operation failed", err, closeErr, removeErr)
		}
		b.dir, b.root, b.identity = dir, root, identity
	}
	name := "part-" + rand.Text()
	file, err := b.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, "", err
	}
	b.files++
	b.names[name] = struct{}{}
	b.active++
	return file, name, nil
}
func (b *Batch) take(n int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.config.MaxBytes-b.bytes {
		return safeerror.Owned("upload resource limit exceeded", &LimitError{Kind: TotalBytes})
	}
	b.bytes += n
	return nil
}
func (b *Batch) remove(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.root.Remove(name)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		delete(b.names, name)
		return nil
	}
	return err
}
func (b *Batch) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		for !b.closed {
			b.changed.Wait()
		}
		return b.closeErr
	}
	b.closing = true
	for b.active != 0 {
		b.changed.Wait()
	}
	var failures []error
	for reader := range b.readers {
		failures = append(failures, reader.close())
	}
	failures = append(failures, b.cleanup())
	b.closeErr = safeerror.Join("operation failed", failures...)
	b.closed = true
	b.changed.Broadcast()
	return b.closeErr
}

// cleanup runs only after captures/readers have exited, under mu. Retain the
// directory identity for exact-file retries; never recursively sweep.
func (b *Batch) cleanup() error {
	var failures []error
	if b.root == nil && len(b.names) != 0 {
		root, err := os.OpenRoot(b.dir)
		if err != nil {
			return safeerror.WrapFailure(fault.Internal, "upload cleanup directory is unavailable", err)
		}
		identity, err := root.Stat(".")
		if err != nil || b.identity == nil || !os.SameFile(b.identity, identity) {
			_ = root.Close()
			return safeerror.NewFault(fault.Conflict, "upload cleanup directory identity changed")
		}
		b.root = root
	}
	if b.root != nil {
		for name := range b.names {
			err := b.root.Remove(name)
			if err == nil || errors.Is(err, fs.ErrNotExist) {
				delete(b.names, name)
			} else {
				failures = append(failures, err)
			}
		}
		failures = append(failures, b.root.Close())
		b.root = nil
	}
	if len(b.names) == 0 && b.dir != "" {
		identity, err := os.Lstat(b.dir)
		if errors.Is(err, fs.ErrNotExist) {
			b.dir = ""
			return safeerror.Join("upload cleanup failed", failures...)
		}
		if err != nil || b.identity == nil || !os.SameFile(b.identity, identity) {
			return safeerror.Join("upload cleanup failed", append(failures, safeerror.NewFault(fault.Conflict, "upload cleanup directory identity changed"))...)
		}
		err = os.Remove(b.dir)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			b.dir = ""
		} else {
			failures = append(failures, err)
		}
	}
	return safeerror.Join("upload cleanup failed", failures...)
}

// CleanupComplete reports actual removal, independently of response/cancellation.
func (b *Batch) CleanupComplete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed && b.root == nil && b.dir == ""
}

// RetryCleanup retries exact-file cleanup only after Close has drained work.
func (b *Batch) RetryCleanup() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		return safeerror.NewFault(fault.Conflict, "upload batch is still active")
	}
	return b.cleanup()
}
