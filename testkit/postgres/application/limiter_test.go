package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

type mockApplication struct {
	startFn         func(context.Context) error
	shutdownFn      func(context.Context) error
	shutdownTimeout time.Duration
	migrations      []infrastructure.MigrationTarget
}

func (m *mockApplication) Start(ctx context.Context) error {
	if m.startFn != nil {
		return m.startFn(ctx)
	}
	return nil
}

func (m *mockApplication) Shutdown(ctx context.Context) error {
	if m.shutdownFn != nil {
		return m.shutdownFn(ctx)
	}
	return nil
}

func (m *mockApplication) ShutdownTimeout() time.Duration {
	if m.shutdownTimeout > 0 {
		return m.shutdownTimeout
	}
	return 5 * time.Second
}

func (m *mockApplication) Migrations() []infrastructure.MigrationTarget {
	return m.migrations
}

type mockTB struct {
	testing.TB
	name      string
	ctx       context.Context
	cleanups  []func()
	mu        sync.Mutex
	fatalMsgs []string
}

func newMockTB(parent testing.TB, name string, ctx context.Context) *mockTB {
	if ctx == nil {
		ctx = parent.Context()
	}
	return &mockTB{
		TB:   parent,
		name: name,
		ctx:  ctx,
	}
}

func (m *mockTB) Name() string {
	return m.name
}

func (m *mockTB) Context() context.Context {
	return m.ctx
}

func (m *mockTB) Cleanup(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanups = append(m.cleanups, fn)
}

func (m *mockTB) Fatalf(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fatalMsgs = append(m.fatalMsgs, fmt.Sprintf(format, args...))
}

func (m *mockTB) Fatal(args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fatalMsgs = append(m.fatalMsgs, fmt.Sprint(args...))
}

func (m *mockTB) finish() {
	m.mu.Lock()
	cleanups := make([]func(), len(m.cleanups))
	copy(cleanups, m.cleanups)
	m.mu.Unlock()

	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}

func makeTestEnvironment(t testing.TB) *Environment {
	scope, s := testScope()
	env, err := Bind(s, selection(scope, "main", "audit"))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestLimiter_BoundRespectedUnderContention(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "3")

	env := makeTestEnvironment(t)
	const totalGoroutines = 9
	var active atomic.Int32
	var peak atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < totalGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			mock := newMockTB(t, fmt.Sprintf("TestContention_%d", id), nil)
			app := &mockApplication{
				startFn: func(ctx context.Context) error {
					cur := active.Add(1)
					for {
						p := peak.Load()
						if cur <= p || peak.CompareAndSwap(p, cur) {
							break
						}
					}
					time.Sleep(20 * time.Millisecond)
					active.Add(-1)
					return nil
				},
			}
			if err := env.Start(mock, app); err != nil {
				t.Errorf("goroutine %d Start error: %v", id, err)
			}
			mock.finish()
		}(i)
	}

	wg.Wait()
	if p := peak.Load(); p != 3 {
		t.Fatalf("expected peak concurrency 3, got %d", p)
	}
}

func TestLimiter_MultiAppAndSubtestsHoldOneSlotWithoutDeadlock(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "2")

	env := makeTestEnvironment(t)

	// Test A boots two applications directly and a third in a subtest.
	mockParent := newMockTB(t, "TestMultiAppGroup", nil)
	mockSub := newMockTB(t, "TestMultiAppGroup/subtest_1", nil)

	appA1 := &mockApplication{}
	appA2 := &mockApplication{}
	appA3 := &mockApplication{}

	if err := env.Start(mockParent, appA1); err != nil {
		t.Fatal(err)
	}
	if err := env.Start(mockParent, appA2); err != nil {
		t.Fatal(err)
	}
	if err := env.Start(mockSub, appA3); err != nil {
		t.Fatal(err)
	}

	// At this point, TestMultiAppGroup has 3 apps running under 1 slot with refs = 3.
	limiterMu.Lock()
	h, exists := limiterHolders["TestMultiAppGroup"]
	if !exists || h.refs != 3 {
		limiterMu.Unlock()
		t.Fatalf("expected holder with refs=3, got exists=%v, holder=%+v", exists, h)
	}
	limiterMu.Unlock()

	// Test B boots its own app, taking the 2nd slot.
	mockB := newMockTB(t, "TestSecondGroup", nil)
	appB := &mockApplication{}
	if err := env.Start(mockB, appB); err != nil {
		t.Fatal(err)
	}

	// Semaphore is now at full capacity (2).
	// A third top-level test must block.
	ctxTimeout, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	mockC := newMockTB(t, "TestThirdGroup", ctxTimeout)
	appC := &mockApplication{}
	err := env.Start(mockC, appC)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded waiting for full capacity, got: %v", err)
	}

	// Now finish appA1 and appA2. The slot for TestMultiAppGroup should remain held (refs = 1).
	// Because cleanups are registered in mockParent:
	mockParent.finish()
	limiterMu.Lock()
	h, exists = limiterHolders["TestMultiAppGroup"]
	if !exists || h.refs != 1 {
		limiterMu.Unlock()
		t.Fatalf("expected holder to persist for subtest with refs=1, got exists=%v, holder=%+v", exists, h)
	}
	limiterMu.Unlock()

	// Now finish the subtest. The slot is freed.
	mockSub.finish()
	limiterMu.Lock()
	_, exists = limiterHolders["TestMultiAppGroup"]
	if exists {
		limiterMu.Unlock()
		t.Fatal("expected TestMultiAppGroup holder to be removed after all apps finish")
	}
	limiterMu.Unlock()

	// Clean up Test B as well.
	mockB.finish()
}

func TestLimiter_CancellationWhileWaitingReturnsContextError(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "1")

	env := makeTestEnvironment(t)

	// Test 1 acquires the single slot.
	mock1 := newMockTB(t, "TestFirstOwner", nil)
	app1 := &mockApplication{}
	if err := env.Start(mock1, app1); err != nil {
		t.Fatal(err)
	}

	// Test 2 attempts to acquire with a canceling context.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	mock2 := newMockTB(t, "TestSecondWaiting", ctx)
	app2 := &mockApplication{}

	start := time.Now()
	err := env.Start(mock2, app2)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
	if elapsed < 25*time.Millisecond {
		t.Fatalf("returned prematurely: %v", elapsed)
	}

	// Verify no slot leaked: after mock1 finishes, a new test can acquire immediately.
	mock1.finish()

	mock3 := newMockTB(t, "TestThirdOwner", nil)
	app3 := &mockApplication{}
	if err := env.Start(mock3, app3); err != nil {
		t.Fatalf("expected immediate acquisition after release, got error: %v", err)
	}
	mock3.finish()
}

func TestLimiter_ReleaseHappensAfterAppShutdown(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "1")

	env := makeTestEnvironment(t)
	mock := newMockTB(t, "TestShutdownOrdering", nil)

	var events []string
	var mu sync.Mutex
	logEvent := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	app := &mockApplication{
		shutdownFn: func(ctx context.Context) error {
			// During shutdown, the slot must still be held!
			limiterMu.Lock()
			_, exists := limiterHolders["TestShutdownOrdering"]
			limiterMu.Unlock()
			if !exists {
				t.Error("limiter holder was released BEFORE app shutdown finished")
			}
			logEvent("shutdown")
			return nil
		},
	}

	if err := env.Start(mock, app); err != nil {
		t.Fatal(err)
	}

	// Run cleanup
	mock.finish()

	limiterMu.Lock()
	_, exists := limiterHolders["TestShutdownOrdering"]
	limiterMu.Unlock()
	if exists {
		t.Error("limiter holder was not cleaned up after finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0] != "shutdown" {
		t.Fatalf("unexpected events: %v", events)
	}
}

func TestLimiter_InvalidEnvironmentValueFailsClearly(t *testing.T) {
	for _, invalid := range []string{"abc", "0", "-1", "1.5"} {
		t.Run("val_"+invalid, func(t *testing.T) {
			resetLimiterForTest(t)
			t.Setenv(concurrencyVariable, invalid)

			env := makeTestEnvironment(t)
			mock := newMockTB(t, "TestInvalidEnv", nil)
			app := &mockApplication{}

			err := env.Start(mock, app)
			var failure *fault.Error
			if !errors.As(err, &failure) || failure.Code() != fault.Invalid || !strings.Contains(failure.Message(), concurrencyVariable) {
				t.Fatalf("Start with %s=%q = %v, want an invalid fault naming the variable", concurrencyVariable, invalid, err)
			}
		})
	}
}

func TestLimiter_ParallelSubtestsRace(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "4")

	env := makeTestEnvironment(t)
	const parallelSubtests = 4
	var wg sync.WaitGroup
	subtests := make([]*mockTB, parallelSubtests)

	for i := 0; i < parallelSubtests; i++ {
		subtests[i] = newMockTB(t, fmt.Sprintf("TestRacingGroup/sub_%d", i), nil)
	}

	for i := 0; i < parallelSubtests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			app := &mockApplication{}
			if err := env.Start(subtests[idx], app); err != nil {
				t.Errorf("subtest %d start error: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Exactly 1 holder in the map, with refs == 4
	limiterMu.Lock()
	h, exists := limiterHolders["TestRacingGroup"]
	if !exists || h.refs != parallelSubtests {
		limiterMu.Unlock()
		t.Fatalf("expected 1 holder with refs=%d, got exists=%v, holder=%+v", parallelSubtests, exists, h)
	}
	// And limiterSem channel length should be exactly 1!
	semLen := len(limiterSem)
	limiterMu.Unlock()

	if semLen != 1 {
		t.Fatalf("expected exactly 1 token in semaphore for racing group, got %d", semLen)
	}

	for _, sub := range subtests {
		sub.finish()
	}

	limiterMu.Lock()
	_, exists = limiterHolders["TestRacingGroup"]
	semLen = len(limiterSem)
	limiterMu.Unlock()

	if exists {
		t.Fatal("expected holder to be deleted after all racing subtests finish")
	}
	if semLen != 0 {
		t.Fatalf("expected 0 tokens in semaphore after all finish, got %d", semLen)
	}
}

func resetLimiterForTest(t testing.TB) {
	t.Helper()
	limiterMu.Lock()
	prevInit := limiterInit
	prevErr := limiterErr
	prevHolders := limiterHolders
	prevSem := limiterSem
	limiterInit = new(sync.Once)
	limiterErr = nil
	limiterHolders = make(map[string]*holder)
	limiterSem = nil
	limiterMu.Unlock()

	t.Cleanup(func() {
		limiterMu.Lock()
		defer limiterMu.Unlock()
		limiterInit = prevInit
		limiterErr = prevErr
		limiterHolders = prevHolders
		limiterSem = prevSem
	})
}
