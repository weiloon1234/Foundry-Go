package query

import "context"

// contextCompiler compiles with the executing call's context, so context
// global scopes of every model source resolve. Plan inspection uses it for all
// plan sources; ordinary Compile uses a context bound with WithScopeContext.
type contextCompiler interface {
	compileIn(context.Context) (Statement, error)
}

func (q Query[M]) compileIn(ctx context.Context) (Statement, error) {
	return q.inContext(ctx).Compile()
}
func (q readResult[R]) compileIn(ctx context.Context) (Statement, error) {
	q.scopeContext = ctx
	return q.Compile()
}
func (q ProjectionQuery[S, P]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q SetQuery[P]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q ValueSetQuery[P]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q ValueQuery[S, V]) compileIn(ctx context.Context) (Statement, error) {
	return q.query.compileIn(ctx)
}
func (q LockedQuery[M]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q LockedResult[S, R]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q TransactionQuery[S, R]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q TransactionValueQuery[S, V]) compileIn(ctx context.Context) (Statement, error) {
	return q.reader().compileIn(ctx)
}
func (q LockedTransactionValue[S, V]) compileIn(ctx context.Context) (Statement, error) {
	return q.query.compileIn(ctx)
}
func (q CursorQuery[R]) compileIn(ctx context.Context) (Statement, error) {
	q, _, _, err := q.canonical()
	if err != nil {
		return Statement{}, err
	}
	return q.reader().compileIn(ctx)
}

// compileSource prefers context compilation for plan and page sources.
func compileSource(ctx context.Context, source interface{ Compile() (Statement, error) }) (Statement, error) {
	if contextual, ok := source.(contextCompiler); ok {
		return contextual.compileIn(ctx)
	}
	return source.Compile()
}
