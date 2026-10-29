package retrievalqueries

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"runtime"

	"github.com/weiloon1234/Foundry-Go/database"
)

var Veto = errors.New("retrieval fixture veto")

type Trace struct {
	Calls          []string
	Names          []string
	Contexts       []context.Context
	LocalFactories int
	FailAt         string
	Mode           string
	Cancel         context.CancelFunc
}

type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

func memberRetrieved(count func(context.Context, database.Executor) (int64, error)) func(context.Context, database.Executor, Member) error {
	first := true
	return func(ctx context.Context, executor database.Executor, member Member) error {
		if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && first {
			trace.LocalFactories++
		}
		first = false
		return observe(ctx, executor, "local", member.Name, count)
	}
}

// Observe performs scalar I/O on the caller's executor, without recursively
// hydrating models. Trace state belongs to one sequential read operation.
func Observe(ctx context.Context, executor database.Executor, label, stored string) error {
	return observe(ctx, executor, label, stored, func() func(arg0 context.Context, arg1 database.Executor) (int64, error) {
		receiver := Members.Query()
		return func(arg0 context.Context, arg1 database.Executor) (int64, error) {
			return orm.Count(arg0, arg1, receiver)
		}
	}())
}

func observe(ctx context.Context, executor database.Executor, label, stored string, count func(context.Context, database.Executor) (int64, error)) error {
	trace, _ := ctx.Value(traceKey{}).(*Trace)
	if trace == nil {
		return nil
	}
	trace.Calls = append(trace.Calls, label)
	trace.Names = append(trace.Names, stored)
	trace.Contexts = append(trace.Contexts, ctx)
	if _, err := count(ctx, executor); err != nil {
		return err
	}
	if trace.FailAt != label {
		return nil
	}
	switch trace.Mode {
	case "panic":
		panic("retrieval fixture panic")
	case "goexit":
		runtime.Goexit()
		return nil
	case "cancel":
		trace.Cancel()
		return nil
	default:
		return Veto
	}
}
