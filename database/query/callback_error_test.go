package query

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type hostileQueryCallbackError struct {
	mode  string
	calls int
}

func (e *hostileQueryCallbackError) Error() string {
	e.calls++
	if e.mode == "exit" {
		runtime.Goexit()
	}
	if e.mode == "panic" {
		panic("private query error")
	}
	return "private query error"
}
func (e *hostileQueryCallbackError) Format(fmt.State, rune) { _ = e.Error() }
func TestScopeAndExplainNeverFormatReturnedErrors(t *testing.T) {
	for _, mode := range []string{"private", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &hostileQueryCallbackError{mode: mode}
			scope := NewContextScope[cursorRecord]("private", func(context.Context) (Predicate[cursorRecord], error) { return Predicate[cursorRecord]{}, cause })
			_, scopeErr := scopedQuery(scope).WithScopeContext(t.Context()).Compile()
			executor := &planExecutor{failure: cause}
			_, explainErr := cursorQuery().Explain(t.Context(), executor)
			for _, err := range []error{scopeErr, explainErr} {
				var retained *hostileQueryCallbackError
				if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("callback cause lost")
				}
				_ = fmt.Sprintf("%v %+v %#v", err, err, err)
			}
			if cause.calls != 0 || executor.calls != 1 {
				t.Fatal("callback formatting or execution changed")
			}
		})
	}
}
func TestContextScopeGoexitContained(t *testing.T) {
	scope := NewContextScope[cursorRecord]("exit", func(context.Context) (Predicate[cursorRecord], error) {
		runtime.Goexit()
		return Predicate[cursorRecord]{}, nil
	})
	_, err := scopedQuery(scope).WithScopeContext(t.Context()).Compile()
	if !errors.Is(err, fault.Panicked) {
		t.Fatal("scope Goexit not contained")
	}
}
