package database

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// WithStickyReads makes routed reads read-your-writes within a request scope:
// after a write through this DB in a context marked by StickyReads, reads that
// would use the read pool use the primary for window. Writes are Exec,
// transactions that are not read-only (including savepoints' outer
// transaction) and framework autocommit statements. Without a marked context
// or a read pool the option has no effect.
func WithStickyReads(window time.Duration) Option {
	return func(settings *poolSettings) error {
		if window <= 0 {
			return fault.New(fault.Invalid, "sticky read window must be positive")
		}
		settings.stickyWindow = window
		return nil
	}
}

type stickyKey struct{}

// stickyScope records the last write issued in one request scope.
type stickyScope struct{ lastWrite atomic.Int64 }

// StickyReads returns a context that tracks writes for read-your-writes
// routing. Use one per request (see StickyReadsHandler); a context that
// already carries a scope is returned unchanged, so nested calls share it.
func StickyReads(ctx context.Context) context.Context {
	if _, ok := ctx.Value(stickyKey{}).(*stickyScope); ok {
		return ctx
	}
	return context.WithValue(ctx, stickyKey{}, &stickyScope{})
}

// StickyReadsHandler gives every request its own StickyReads scope. Add it to
// the HTTP middleware stack of applications whose database uses
// WithStickyReads; background work may call StickyReads explicitly.
func StickyReadsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(StickyReads(r.Context())))
	})
}

// markWrite notes a write in the request scope when stickiness is enabled.
func (db *DB) markWrite(ctx context.Context) {
	if db == nil || db.stickyWindow <= 0 || ctx == nil {
		return
	}
	if scope, ok := ctx.Value(stickyKey{}).(*stickyScope); ok {
		scope.lastWrite.Store(time.Now().UnixNano())
	}
}

// markOnClose returns a wrapper that marks the request scope again when a
// primary stream closes.
func (db *DB) markOnClose(ctx context.Context) func(*Rows, error) (*Rows, error) {
	return func(rows *Rows, err error) (*Rows, error) {
		if err != nil || rows == nil || db == nil || db.stickyWindow <= 0 {
			return rows, err
		}
		rows.mu.Lock()
		rows.afterClose = func() { db.markWrite(ctx) }
		rows.mu.Unlock()
		return rows, nil
	}
}

// readsStickToPrimary reports a write in this scope within the window.
func (db *DB) readsStickToPrimary(ctx context.Context) bool {
	if db.stickyWindow <= 0 || db.read == nil {
		return false
	}
	scope, ok := ctx.Value(stickyKey{}).(*stickyScope)
	if !ok {
		return false
	}
	last := scope.lastWrite.Load()
	return last != 0 && time.Since(time.Unix(0, last)) < db.stickyWindow
}
