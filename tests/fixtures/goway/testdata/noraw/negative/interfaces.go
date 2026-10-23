package negative

import (
	"context"

	db "github.com/weiloon1234/Foundry-Go/database"
)

type Runner[R any] interface {
	Query(context.Context, string, ...any) (R, error)
}

func Interfaces(ctx context.Context, ex *db.DB) {
	var generic Runner[*db.Rows] = ex
	_, _ = generic.Query(ctx, "private_sql_canary") // want database.Query
	generic = ex
	converted := Runner[*db.Rows](ex)
	_, _ = converted.Query(ctx, "private_sql_canary") // want database.Query
	var run func(context.Context, string, ...any) (*db.Rows, error) = generic.Query
	_, _ = run(ctx, "private_sql_canary") // want database.Query
	var plain localExecutor = ex
	_, _ = plain.Query(ctx, "private_sql_canary") // want database.Query
	plain = localExecutor(ex)
	method := plain.Query
	_, _ = method(ctx, "private_sql_canary") // want database.Query
	accept := func(r Runner[*db.Rows]) {
		_, _ = r.Query(ctx, "private_sql_canary") // want database.Query
	}
	accept(ex)
	// A function variable still has a resolved parameter contract.
	var consume func(localExecutor) = func(localExecutor) {}
	consume(ex)
	_ = []Runner[*db.Rows]{ex}
	_ = map[string]localExecutor{"db": ex}
	_ = struct{ Run localExecutor }{Run: ex}
	_ = struct{ Run localExecutor }{ex}
	channel := make(chan localExecutor, 1)
	channel <- ex
	var multi localExecutor
	multi, _ = executorPair(ex)
	_ = multi
}
func executorPair(ex *db.DB) (*db.DB, bool) { return ex, true }
func InterfaceReturn(ex *db.DB) Runner[*db.Rows] {
	return ex
}
func GenericRestatement[R any](ctx context.Context, r Runner[R]) {
	// Return type R stays generic; the executor parameter shape is still raw.
	_, _ = r.Query(ctx, "private_sql_canary") // want database.Query
}
func InferredInterface(ctx context.Context, ex *db.DB) {
	GenericRestatement(ctx, ex)
}

func Constrained[R any, E interface {
	Query(context.Context, string, ...any) (R, error)
}](ctx context.Context, ex E) {
	_, _ = ex.Query(ctx, "private_sql_canary") // want database.Query
	run := ex.Query
	alias := run
	// R is unresolved: this value does not have an identical raw signature.
	_, _ = alias(ctx, "private_sql_canary")
}
