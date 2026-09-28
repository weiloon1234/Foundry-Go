package upload

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
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
	names           map[string]struct{}
	readers         map[*Reader]struct{}
	closeErr        error
}

func New(ctx context.Context, config Config) (*Batch, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "upload batch requires a context")
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
		return nil, "", &LimitError{Kind: Files}
	}
	if b.root == nil {
		dir, err := os.MkdirTemp(b.config.TempDirectory, "foundry-upload-")
		if err != nil {
			return nil, "", err
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			removeErr := os.Remove(dir)
			return nil, "", errors.Join(err, removeErr)
		}
		b.dir, b.root = dir, root
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
		return &LimitError{Kind: TotalBytes}
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
	if b.root != nil {
		for name := range b.names {
			err := b.root.Remove(name)
			if !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, err)
			}
		}
		failures = append(failures, b.root.Close(), os.Remove(b.dir))
	}
	b.closeErr = errors.Join(failures...)
	b.closed = true
	b.changed.Broadcast()
	return b.closeErr
}
