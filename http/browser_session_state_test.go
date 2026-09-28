package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func sessionState(t *testing.T) (context.Context, *browserSessionState) {
	t.Helper()
	owner, cancel := context.WithCancel(t.Context())
	state := &browserSessionState{owner: &browserSessionAdapter{clock: clock.System{}}, context: owner, cancel: cancel, method: "POST"}
	ctx := context.WithValue(t.Context(), browserSessionKey{}, state)
	t.Cleanup(state.close)
	if err := bindBrowserResponse(ctx, true); err != nil {
		t.Fatal(err)
	}
	return ctx, state
}
func TestBrowserSessionOwnsLateMutationAndRejectsEarlyResponse(t *testing.T) {
	ctx, state := sessionState(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- state.operation(context.WithoutCancel(ctx), func(context.Context) (string, time.Time, error) {
			close(entered)
			<-release
			return "credential=private", time.Time{}, nil
		})
	}()
	<-entered
	w := httptest.NewRecorder()
	if err := publishBrowserSession(ctx, w, 204); !errors.Is(err, fault.Conflict) {
		t.Fatal("busy operation published", err)
	}
	closed := make(chan struct{})
	go func() { state.close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("close abandoned mutation")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("late mutation accepted", err)
	}
	<-closed
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("late cookie published")
	}
	if err := state.operation(context.WithoutCancel(ctx), func(context.Context) (string, time.Time, error) {
		t.Error("closed mutation ran")
		return "", time.Time{}, nil
	}); err == nil {
		t.Fatal("closed response accepted mutation")
	}
}
func TestBrowserSessionPublishesOnlyOnePreparedSuccess(t *testing.T) {
	for _, status := range []int{204, 400, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			ctx, state := sessionState(t)
			if err := state.operation(ctx, func(context.Context) (string, time.Time, error) { return "credential=private", time.Time{}, nil }); err != nil {
				t.Fatal(err)
			}
			if err := state.operation(ctx, func(context.Context) (string, time.Time, error) {
				t.Error("second mutation ran")
				return "", time.Time{}, nil
			}); err == nil {
				t.Fatal("multiple mutation allowed")
			}
			w := httptest.NewRecorder()
			if err := publishBrowserSession(ctx, w, status); err != nil {
				t.Fatal(err)
			}
			if (w.Header().Get("Set-Cookie") != "") != (status == 204) {
				t.Fatal("incorrect cookie publication")
			}
			if err := state.operation(ctx, func(context.Context) (string, time.Time, error) { return "", time.Time{}, nil }); err == nil {
				t.Fatal("committed response mutated")
			}
		})
	}
}
func TestBrowserSessionCallbackFailureIsOwned(t *testing.T) {
	for _, fn := range []func(context.Context) (string, time.Time, error){func(context.Context) (string, time.Time, error) { panic("private") }, func(context.Context) (string, time.Time, error) { runtime.Goexit(); return "", time.Time{}, nil }} {
		ctx, state := sessionState(t)
		if err := state.operation(ctx, fn); err == nil {
			t.Fatal("abnormal callback accepted")
		}
		state.close()
	}
}

type browserTestClock func() time.Time

func (f browserTestClock) Now() time.Time { return f() }
func TestBrowserSessionCanceledDuringExpiryCheckWithholdsCookie(t *testing.T) {
	original, state := sessionState(t)
	ctx, cancel := context.WithCancel(original)
	defer cancel()
	if err := state.operation(ctx, func(context.Context) (string, time.Time, error) {
		return "credential=private", time.Now().Add(time.Hour), nil
	}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	state.owner.clock = browserTestClock(func() time.Time { close(entered); <-release; return time.Now() })
	w := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { done <- publishBrowserSession(ctx, w, 204) }()
	<-entered
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("late clock callback published cookie", err)
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("canceled expiry check exposed secret")
	}
}
