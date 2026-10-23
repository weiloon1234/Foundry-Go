package orm

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// GlobalScope is a model-owned, additive default predicate. Keep its value for
// query-local removal. Example: var tenantScope orm.GlobalScope[Order].
type GlobalScope[M any] struct {
	_          [0]*M
	owner      *tableCore
	definition querycore.ScopeDefinition
}

// Scope resolves a predicate with the executing context on every statement.
// A missing tenant must return an error. Example: orm.Scope(Orders.Table, "tenant", func(ctx context.Context) (orm.Predicate[Order], error) { id, err := tenantID(ctx); return Orders.TenantID.Eq(id), err }).
func Scope[M any, K comparable](table Table[M, K], name string, resolve func(context.Context) (Predicate[M], error)) GlobalScope[M] {
	definition := querycore.ScopeDefinition{Name: querycore.GlobalScopeName(name)}
	if resolve != nil {
		definition.Resolve = func(ctx context.Context) (querycore.Expression, error) {
			p, err := resolve(ctx)
			if err != nil {
				return nil, lifecore.Failure("model global scope failed", err)
			}
			if p.owner == nil || p.owner != table.core {
				return nil, invalid("global scope predicate belongs to a different table declaration")
			}
			return p.expression, p.err
		}
	}
	return GlobalScope[M]{owner: table.core, definition: definition}
}

// WithGlobalScopes attaches a lazy immutable source, evaluated once when needed.
// The source receives an owned copy of the completed declaration, avoiding Go
// initialization cycles. It may reference relations; it must not
// execute or compile queries. Example: WithGlobalScopes(func(t OrderTable) []GlobalScope[Order] { return []GlobalScope[Order]{Scope(t.Table, "tenant", tenantPredicate(t))} }).
func WithGlobalScopes[M any, T CheckedTable](source func(T) []GlobalScope[M]) Option {
	opt := Option{scopeModel: reflect.TypeFor[M](), scopeTable: reflect.TypeFor[T]()}
	if source != nil {
		opt.scopes = func(snapshot any, t *tableCore) []querycore.ScopeDefinition {
			scopes := source(snapshot.(T))
			result := make([]querycore.ScopeDefinition, len(scopes))
			for i, scope := range scopes {
				result[i] = scope.definition
				if scope.owner != t || scope.owner == nil {
					result[i] = querycore.ScopeDefinition{}
				}
			}
			return result
		}
	}
	return opt
}

// Name returns the declared identifier. Example: tenantScope.Name().
func (s GlobalScope[M]) Name() string { return string(s.definition.Name) }

// WithoutGlobalScope removes named defaults from this query only, preserving
// explicit predicates and soft-delete visibility. Example: WithoutGlobalScope(Orders.Query(), orderTenantScope).
func WithoutGlobalScope[M any](q Query[M], scopes ...GlobalScope[M]) Query[M] {
	q.state = withoutGlobalScope(q.state, scopes)
	return q
}
func withoutGlobalScope[M any](q queryState, scopes []GlobalScope[M]) queryState {
	q.ast.WithoutScopes = slices.Clone(q.ast.WithoutScopes)
	for _, scope := range scopes {
		if scope.owner == nil || scope.owner != q.table {
			q.err = errors.Join(q.err, invalid("global scope belongs to a different table declaration"))
		}
		q.ast.WithoutScopes = append(q.ast.WithoutScopes, scope.definition.Name)
	}
	return q
}

// WithoutGlobalScopes removes all model defaults from this query only.
// Example: WithoutGlobalScopes(Orders.Query()).Where(Orders.ID.Eq(id)).
func WithoutGlobalScopes[M any](q Query[M]) Query[M] { q.state.ast.AllScopesOff = true; return q }

// WithScopeContext supplies context for Compile; execution always uses its own
// context. Example: Compile(WithScopeContext(Orders.Query(), ctx)).
func WithScopeContext[M any](q Query[M], ctx context.Context) Query[M] {
	q.state.ast.ScopeContext = ctx
	return q
}
