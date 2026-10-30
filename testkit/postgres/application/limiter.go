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
	refs int
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

	limiterMu.Lock()
	if h, ok := limiterHolders[key]; ok {
		h.refs++
		limiterMu.Unlock()
		return makeRelease(key), nil
	}
	limiterMu.Unlock()

	select {
	case limiterSem <- struct{}{}:
	case <-t.Context().Done():
		return nil, t.Context().Err()
	}

	limiterMu.Lock()
	if h, ok := limiterHolders[key]; ok {
		// A parallel subtest of the same test won the race and created the holder:
		<-limiterSem
		h.refs++
		limiterMu.Unlock()
		return makeRelease(key), nil
	}

	limiterHolders[key] = &holder{refs: 1}
	limiterMu.Unlock()
	return makeRelease(key), nil
}

func makeRelease(key string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			limiterMu.Lock()
			defer limiterMu.Unlock()
			h, ok := limiterHolders[key]
			if !ok {
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
