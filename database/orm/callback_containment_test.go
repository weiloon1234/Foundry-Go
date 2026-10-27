package orm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
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
			table := hookedFields(nil, WithGlobalScopes(func(t hookedTable) []GlobalScope[hookedRow] {
				return []GlobalScope[hookedRow]{Scope(t.Table, "private", func(context.Context) (Predicate[hookedRow], error) { return Predicate[hookedRow]{}, cause })}
			}))
			_, scopeErr := Compile(WithScopeContext(table.Query(), t.Context()))
			executor := &diagnosticPlanExecutor{failure: cause}
			_, explainErr := Explain(t.Context(), executor, hookedFields(nil).Query())
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
	table := hookedFields(nil, WithGlobalScopes(func(t hookedTable) []GlobalScope[hookedRow] {
		return []GlobalScope[hookedRow]{Scope(t.Table, "exit", func(context.Context) (Predicate[hookedRow], error) {
			runtime.Goexit()
			return Predicate[hookedRow]{}, nil
		})}
	}))
	_, err := Compile(WithScopeContext(table.Query(), t.Context()))
	if !errors.Is(err, fault.Panicked) {
		t.Fatal("scope Goexit not contained")
	}
}

type diagnosticPlanExecutor struct {
	failure error
	calls   int
}

func (e *diagnosticPlanExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	e.calls++
	return nil, e.failure
}
func (e *diagnosticPlanExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	e.calls++
	return database.Result{}, e.failure
}
