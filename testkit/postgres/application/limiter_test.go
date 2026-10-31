package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

type mockApplication struct {
	startFn         func(context.Context) error
	shutdownFn      func(context.Context) error
	shutdownTimeout time.Duration
	migrations      []infrastructure.MigrationTarget
	done            chan struct{}
	doneInit        sync.Once
	doneClose       sync.Once
}

func (m *mockApplication) Start(ctx context.Context) error {
	if m.startFn != nil {
		return m.startFn(ctx)
	}
	return nil
}

func (m *mockApplication) Shutdown(ctx context.Context) error {
	var err error
	if m.shutdownFn != nil {
		err = m.shutdownFn(ctx)
	}
	m.doneClose.Do(func() { close(m.completion()) })
	return err
}

func (m *mockApplication) completion() chan struct{} {
	m.doneInit.Do(func() { m.done = make(chan struct{}) })
	return m.done
}

func (m *mockApplication) Done() <-chan struct{} { return m.completion() }

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

type scopedFoundationApplication struct {
	*foundation.App
}

func (scopedFoundationApplication) Migrations() []infrastructure.MigrationTarget { return nil }

func TestLimiter_ShutdownTimeoutRetainsSlotUntilDependenciesClose(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "1")
	env := makeTestEnvironment(t)
	releaseTask := make(chan struct{})
	var unblock sync.Once
	var dependencyClosed atomic.Bool
	startupFailure := errors.New("startup failed after admitting a task")
	app, err := foundation.NewBuilder(
		foundation.WithShutdownTimeout(20*time.Millisecond),
		foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	).Register(foundation.Module{
		Name: "pending-shutdown",
		OnBoot: func(_ context.Context, r *foundation.Runtime) error {
			if err := r.OnShutdown("dependency", func(context.Context) error {
				dependencyClosed.Store(true)
				return nil
			}); err != nil {
				return err
			}
			if err := r.Go("held-task", func(context.Context) error {
				<-releaseTask
				return nil
			}); err != nil {
				return err
			}
			return startupFailure
		},
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unblock.Do(func() { close(releaseTask) })
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		select {
		case <-app.Done():
		case <-ctx.Done():
			t.Error("application did not finish after its task exited")
			return
		}
		if !dependencyClosed.Load() {
			t.Error("application completed without closing its dependency")
		}
		// Also wait for the limiter to observe completion before resetting it.
		release, err := acquireSlot(newMockTB(t, "TestAfterShutdownCompletion", ctx))
		if err != nil {
			t.Errorf("capacity not released after application completion: %v", err)
			return
		}
		release()
	})
	mock := newMockTB(t, "TestPendingShutdown", nil)
	err = env.Start(mock, scopedFoundationApplication{app})
	if !errors.Is(err, startupFailure) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start = %v, want startup failure and shutdown timeout", err)
	}
	if dependencyClosed.Load() {
		t.Fatal("dependency closed while the admitted task still owns it")
	}
	// Repeated cleanup must not release the still-owned slot either.
	mock.finish()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	release, err := acquireSlot(newMockTB(t, "TestBlockedByPendingShutdown", ctx))
	if release != nil {
		release()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire during pending shutdown = %v, want deadline exceeded", err)
	}
}

type waitingTB struct {
	*mockTB
	waiting chan struct{}
	once    sync.Once
}

func (m *waitingTB) Context() context.Context {
	return slotWaitContext{Context: m.mockTB.Context(), caller: m}
}

type slotWaitContext struct {
	context.Context
	caller *waitingTB
}

func (ctx slotWaitContext) Done() <-chan struct{} {
	ctx.caller.once.Do(func() { close(ctx.caller.waiting) })
	return ctx.Context.Done()
}

func TestLimiter_ConcurrentStartsJoinPendingGroupAtCapacityOne(t *testing.T) {
	resetLimiterForTest(t)
	t.Setenv(concurrencyVariable, "1")
	env := makeTestEnvironment(t)
	owner := newMockTB(t, "TestCapacityOwner", nil)
	if err := env.Start(owner, &mockApplication{}); err != nil {
		t.Fatal(err)
	}
	defer owner.finish()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, 2)
	callers := make([]*waitingTB, 2)
	for i := range callers {
		callers[i] = &waitingTB{
			mockTB:  newMockTB(t, fmt.Sprintf("TestPendingGroup/sub_%d", i), ctx),
			waiting: make(chan struct{}),
		}
		go func(caller *waitingTB) { results <- env.Start(caller, &mockApplication{}) }(callers[i])
	}
	for _, caller := range callers {
		select {
		case <-caller.waiting:
		case <-time.After(2 * time.Second):
			cancel()
			t.Error("start did not reach capacity wait")
		}
	}
	owner.finish()
	for range callers {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("concurrent Start = %v", err)
			}
		case <-time.After(time.Second):
			cancel()
			<-results
			t.Error("concurrent starts waited behind their own group's capacity slot")
		}
	}
	for _, caller := range callers {
		caller.finish()
	}
}

func TestLimiter_PendingGroupCancellationDoesNotCancelSibling(t *testing.T) {
	for _, canceled := range []int{0, 1} {
		t.Run(fmt.Sprintf("cancel_caller_%d", canceled), func(t *testing.T) {
			resetLimiterForTest(t)
			t.Setenv(concurrencyVariable, "1")
			owner, err := acquireSlot(newMockTB(t, "TestCapacityOwner", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer owner()
			type result struct {
				release func()
				err     error
			}
			var cancels [2]context.CancelFunc
			var results [2]chan result
			for i := range results {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				cancels[i] = cancel
				results[i] = make(chan result, 1)
				caller := &waitingTB{
					mockTB:  newMockTB(t, fmt.Sprintf("TestPendingCancellation/sub_%d", i), ctx),
					waiting: make(chan struct{}),
				}
				go func() {
					release, err := acquireSlot(caller)
					results[i] <- result{release: release, err: err}
				}()
				// The first caller owns the pending acquisition before the
				// second joins it; both are blocked by the unrelated owner.
				select {
				case <-caller.waiting:
				case <-ctx.Done():
					t.Fatal("caller did not enter the capacity wait")
				}
			}
			cancels[canceled]()
			stopped := <-results[canceled]
			if stopped.release != nil {
				stopped.release()
			}
			if !errors.Is(stopped.err, context.Canceled) {
				t.Fatalf("canceled caller = %v, want context.Canceled", stopped.err)
			}
			owner()
			sibling := <-results[1-canceled]
			if sibling.err != nil {
				t.Fatalf("live sibling inherited another caller's cancellation: %v", sibling.err)
			}
			sibling.release()
			sibling.release()
			limiterMu.Lock()
			holders, slots := len(limiterHolders), len(limiterSem)
			limiterMu.Unlock()
			if holders != 0 || slots != 0 {
				t.Fatalf("pending cancellation leaked capacity: holders=%d slots=%d", holders, slots)
			}
		})
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
