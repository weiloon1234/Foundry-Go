package database

import (
	"log/slog"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Option configures application-owned database runtime dependencies separately
// from connection bounds and adapter credentials.
type Option func(*poolSettings) error

type poolSettings struct {
	clock           clock.Clock
	clockSet        bool
	read            *readPoolSettings
	connectionLimit int
	queryObserver   QueryObserver
	slowLogger      *slog.Logger
	slowThreshold   time.Duration
	stickyWindow    time.Duration
}

// WithClock injects model lifecycle time. Direct Open/Prepare calls default to
// clock.System; database Modules inherit the application's clock unless this
// option supplies an explicit override. Connection and cleanup deadlines always
// use real context deadlines. The source must be safe for concurrent use.
func WithClock(source clock.Clock) Option {
	return func(settings *poolSettings) error {
		if source == nil {
			return fault.New(fault.Invalid, "database clock cannot be nil")
		}
		settings.clock, settings.clockSet = source, true
		return nil
	}
}

func configurePool(options []Option) (poolSettings, error) {
	settings := poolSettings{clock: clock.System{}}
	for _, option := range options {
		if option == nil {
			return poolSettings{}, fault.New(fault.Invalid, "database option cannot be nil")
		}
		if err := option(&settings); err != nil {
			return poolSettings{}, err
		}
	}
	return settings, nil
}

// bindRuntimeClock completes module time ownership before startup. A prepared
// module cannot be started by another provider before its own boot callback.
func (db *DB) bindRuntimeClock(source clock.Clock) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closing || db.startAttempted || db.clockBound {
		return fault.New(fault.Closed, "database clock binding is frozen")
	}
	if source == nil {
		return fault.New(fault.Invalid, "application clock cannot be nil")
	}
	if !db.clockExplicit {
		db.timeSource = source
	}
	db.clockBound = true
	return nil
}

// Clock returns the pool's application time source. A prepared Module inherits
// its application source during boot; after Start it never replaces that source.
// Reading the clock does not acquire a connection or extend resource ownership.
func (db *DB) Clock() clock.Clock {
	if db.frozen.Load() {
		return db.timeSource
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.timeSource
}

// Clock retains the owning pool's time source on a connection-scoped session.
func (s *Session) Clock() clock.Clock { return s.timeSource }

// Clock retains the actual transaction owner's time source through savepoints
// and custom transactor wrappers. Its time is not frozen at transaction start.
func (tx *Tx) Clock() clock.Clock { return tx.timeSource }
