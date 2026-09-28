package validation_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelOverlapsAndPreservesOrderAndLabels(t *testing.T) {
	entered := make(chan int, 4)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	branches := make([]validation.Rule[string], 0, 8)
	for i := range 8 {
		rule := validation.Custom(validation.Spec{ID: validation.RuleID("app.branch_" + strconv.Itoa(i)), Message: "Invalid."}, func(context.Context, string) (bool, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); count > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, count) {
					break
				}
			}
			entered <- i
			<-release
			return false, nil
		})
		field := validation.DefineField("f"+strconv.Itoa(i), func(v string) string { return v }).WithLabel("Field " + strconv.Itoa(i))
		branches = append(branches, field.Rules(validation.Parallel(rule)))
	}
	done := make(chan error, 1)
	go func() {
		done <- validation.Parallel(branches...).Check(t.Context(), "input", validation.DefaultLimits())
	}()
	for range 4 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("parallel checks did not overlap")
		}
	}
	close(release)
	err := <-done
	issues := rejection(t, err).Issues()
	if maximum.Load() != 4 || active.Load() != 0 || len(issues) != 8 {
		t.Fatal("worker bound or drain", maximum.Load(), active.Load(), len(issues))
	}
	for i, issue := range issues {
		if issue.Path != "/f"+strconv.Itoa(i) || issue.Label != "Field "+strconv.Itoa(i) {
			t.Fatal("unstable diagnostics", issues)
		}
	}
}

func TestParallelCancellationWaitsForCallbackExitAndCanReuse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	slow := validation.Custom(validation.Spec{ID: "app.slow", Message: "Invalid."}, func(ctx context.Context, _ string) (bool, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return true, ctx.Err()
	})
	rule := validation.Parallel(slow, validation.NonBlank[string]())
	done := make(chan error, 1)
	go func() { done <- rule.Check(ctx, "value", validation.DefaultLimits()) }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("abandoned owned callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := rule.Check(t.Context(), "value", validation.DefaultLimits()); err != nil {
		t.Fatal("capacity not reusable", err)
	}
}

func TestParallelFaultsLimitsConditionsAndPreflight(t *testing.T) {
	for _, check := range []func(context.Context, string) (bool, error){
		func(context.Context, string) (bool, error) { panic("private") },
		func(context.Context, string) (bool, error) { runtime.Goexit(); return true, nil },
		func(context.Context, string) (bool, error) { return false, errors.New("private") },
	} {
		bad := validation.Custom(validation.Spec{ID: "app.fail", Message: "Invalid."}, check)
		limits := validation.DefaultLimits()
		limits.Issues = 1
		err := validation.Parallel(validation.NonBlank[string](), bad).Check(t.Context(), "", limits)
		if !errors.Is(err, fault.Internal) {
			t.Fatal("fault lost behind issue cap", err)
		}
	}
	leaves := []validation.Rule[string]{}
	for range 20 {
		leaves = append(leaves, validation.NonBlank[string]())
	}
	rule := validation.Parallel(leaves...)
	limits := validation.DefaultLimits()
	limits.Checks = 10
	var exceeded *validation.LimitError
	if !errors.As(rule.Check(t.Context(), "ok", limits), &exceeded) {
		t.Fatal("parallel multiplied budget")
	}
	limits = validation.DefaultLimits()
	limits.Issues = 2
	errs := rejection(t, rule.Check(t.Context(), "", limits))
	if len(errs.Issues()) != 2 || !errs.Truncated() {
		t.Fatal("issue cap lost")
	}
	condition := validation.When(validation.OneOf("ok"), validation.NonBlank[string]())
	if err := validation.Parallel(condition, condition).Check(t.Context(), "ok", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	prohibited := validation.Parallel(validation.Prohibited[string](), validation.Optional(validation.NonBlank[string]()))
	rejection(t, prohibited.CheckProhibitions(t.Context(), value.Set("supplied"), validation.DefaultLimits()))
	for _, limit := range []int{0, 65} {
		if validation.ParallelLimit(limit, validation.NonBlank[string]()).Validate() == nil {
			t.Fatal("invalid concurrency accepted")
		}
	}
}

func TestParallelBailAvoidsIOAfterCheapFailure(t *testing.T) {
	var calls atomic.Int32
	remote := validation.Custom(validation.Spec{ID: "app.remote", Message: "Invalid."}, func(context.Context, string) (bool, error) { calls.Add(1); return true, nil })
	rule := validation.Parallel(validation.Bail(validation.Email[string](), remote), validation.Bail(validation.Email[string](), remote))
	rejection(t, rule.Check(t.Context(), "bad", validation.DefaultLimits()))
	if calls.Load() != 0 {
		t.Fatal("bail performed remote work")
	}
}
