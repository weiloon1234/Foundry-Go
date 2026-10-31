package application

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// DefaultConcurrency is the default maximum number of concurrently booted
// applications per test process. At 4 connections per booted app (2 primary +
// 2 read), 8 concurrent applications use 32 connections, leaving 68 connections
// headroom below PostgreSQL's default max_connections ceiling of 100 for
// schema creation, migrations, and assertion pools.
//
// The limit may be overridden via the FOUNDRY_TEST_APP_CONCURRENCY environment
// variable with a positive integer.
const DefaultConcurrency = 8

const concurrencyVariable = "FOUNDRY_TEST_APP_CONCURRENCY"

type holder struct {
	refs     int
	ready    chan struct{}
	acquired bool
}

var (
	limiterMu      sync.Mutex
	limiterSem     chan struct{}
	limiterHolders = make(map[string]*holder)
	limiterInit    = new(sync.Once)
	limiterErr     error
)

func initLimiter() error {
	limiterInit.Do(func() {
		limit := DefaultConcurrency
		val := os.Getenv(concurrencyVariable)
		if val != "" {
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				limiterErr = fault.New(fault.Invalid, concurrencyVariable+" must be a positive integer")
				return
			}
			limit = n
		}
		limiterSem = make(chan struct{}, limit)
	})
	return limiterErr
}

func topLevelTestName(t testing.TB) string {
	if t == nil {
		return "unknown"
	}
	name := t.Name()
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return "unknown"
	}
	return name
}

func acquireSlot(t testing.TB) (func(), error) {
	if err := initLimiter(); err != nil {
		return nil, err
	}

	key := topLevelTestName(t)
	ctx := t.Context()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limiterMu.Lock()
		h, exists := limiterHolders[key]
		if !exists {
			// Publish the pending group before waiting for global capacity.
			// Siblings reserve references and join this acquisition.
			h = &holder{ready: make(chan struct{})}
			limiterHolders[key] = h
		}
		h.refs++
		limiterMu.Unlock()

		if !exists {
			select {
			case limiterSem <- struct{}{}:
				h.acquired = true
			case <-ctx.Done():
			}
			limiterMu.Lock()
			if !h.acquired {
				delete(limiterHolders, key)
			}
			close(h.ready)
			limiterMu.Unlock()
			if !h.acquired {
				return nil, ctx.Err()
			}
		}

		release := makeRelease(key, h)
		select {
		case <-h.ready:
			if !h.acquired {
				// The acquiring caller was canceled. A live sibling retries
				// with its own context instead of inheriting that cancellation.
				continue
			}
			if err := ctx.Err(); err != nil {
				release()
				return nil, err
			}
			return release, nil
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
}

func makeRelease(key string, h *holder) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			limiterMu.Lock()
			defer limiterMu.Unlock()
			if limiterHolders[key] != h {
				return
			}
			h.refs--
			if h.refs <= 0 {
				delete(limiterHolders, key)
				<-limiterSem
			}
		})
	}
}
