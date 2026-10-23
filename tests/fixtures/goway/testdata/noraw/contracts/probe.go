// This consumer intentionally imports no framework package.
package contracts

import "context"

type Runner[R any] interface {
	Query(context.Context, string, ...any) (R, error)
}

func Run[R any](ctx context.Context, r Runner[R]) {
	_, _ = r.Query(ctx, "private_sql_canary") // want database.Query
	f := r.Query
	// An unresolved result type is not an identical framework raw signature.
	_, _ = f(ctx, "private_sql_canary")
}
func Generic[R any, E interface {
	QueryRead(context.Context, string, ...any) (R, error)
}](ctx context.Context, ex E) {
	_, _ = ex.QueryRead(ctx, "private_sql_canary") // want database.QueryRead
}
func Scan(ctx context.Context, s interface {
	ScanOne(context.Context, string, []any, ...any) error
}) {
	_ = s.ScanOne(ctx, "private_sql_canary", nil) // want database.ScanOne
}
