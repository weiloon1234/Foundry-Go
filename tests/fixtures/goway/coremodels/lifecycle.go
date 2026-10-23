package coremodels

import "context"

type lifecycleLogKey struct{}

// LifecycleContext supplies an operation's test trace; declarations retain no mutable registry.
func LifecycleContext(ctx context.Context, log *[]string) context.Context {
	return context.WithValue(ctx, lifecycleLogKey{}, log)
}

// TraceLifecycle records lifecycle order in the fixture operation.
func TraceLifecycle(ctx context.Context, stage string) {
	if log, ok := ctx.Value(lifecycleLogKey{}).(*[]string); ok {
		*log = append(*log, stage)
	}
}
