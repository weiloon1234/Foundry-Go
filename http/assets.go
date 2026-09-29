package http

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Assets owns a prepared asset source. Directory handles stay open until all
// acquired file bodies and lookup operations have finished. Close stops new
// acquisitions, waits for existing owners and never interrupts a file read.
// AssetsModule owns this lifecycle in applications; OpenAssets is for explicit
// standalone use and tests. Assets must not be copied after construction.
type Assets struct {
	config           AssetsConfig
	mu               sync.Mutex
	root             *os.Root
	started, closing bool
	active           int
	done             chan struct{}
	closeOnce        sync.Once
	closeErr         error
	// stats briefly caches successful lookups; tags caches content validators
	// for files without a modification time, such as embed.FS entries.
	stats assetStatCache
	tags  sync.Map // string -> assetTag
}

func prepareAssets(config AssetsConfig) (*Assets, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Assets{config: config.snapshot(), done: make(chan struct{})}, nil
}
func OpenAssets(ctx context.Context, config AssetsConfig) (*Assets, error) {
	assets, err := prepareAssets(config)
	if err != nil {
		return nil, err
	}
	if err := assets.start(ctx); err != nil {
		return nil, err
	}
	return assets, nil
}
func (a *Assets) start(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "assets require a context")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started || a.closing {
		return fault.New(fault.Conflict, "assets already started or closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.config.Source.directory != "" {
		root, err := os.OpenRoot(a.config.Source.directory)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(err, root.Close())
		}
		a.root = root
	}
	a.started = true
	return nil
}
func (a *Assets) acquire(ctx context.Context) (func(), error) {
	if a == nil || ctx == nil {
		return nil, Unavailable
	}
	// Asset requests carry no body: a deadline here is the server's own budget.
	if err := ctx.Err(); err != nil {
		return nil, Unavailable.WithCause(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started || a.closing {
		return nil, Unavailable
	}
	a.active++
	var once sync.Once
	return func() { once.Do(a.release) }, nil
}
func (a *Assets) release() {
	a.mu.Lock()
	a.active--
	finish := a.closing && a.active == 0
	a.mu.Unlock()
	if finish {
		a.finishClose()
	}
}
func (a *Assets) finishClose() {
	a.closeOnce.Do(func() {
		var err error
		if a.root != nil {
			err = a.root.Close()
		}
		a.mu.Lock()
		a.closeErr = err
		close(a.done)
		a.mu.Unlock()
	})
}

// Close is idempotent. A canceled wait does not reopen the source or abandon
// its handles: the final owner completes closure and later Close calls observe it.
func (a *Assets) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "assets close requires a context")
	}
	a.mu.Lock()
	if a.done == nil {
		a.mu.Unlock()
		return fault.New(fault.Invalid, "assets are not constructed")
	}
	a.closing = true
	finish := a.active == 0
	done := a.done
	a.mu.Unlock()
	if finish {
		a.finishClose()
	}
	select {
	case <-done:
		a.mu.Lock()
		err := a.closeErr
		a.mu.Unlock()
		return err
	default:
	}
	select {
	case <-done:
		a.mu.Lock()
		err := a.closeErr
		a.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
