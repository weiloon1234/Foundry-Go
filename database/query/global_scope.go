package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// GlobalScopeName identifies one model-declared default scope. It uses the SQL
// identifier grammar so diagnostics never echo arbitrary text.
type GlobalScopeName querycore.GlobalScopeName

type lazyScopes = querycore.LazyScopes

func validateScopes(scopes []scopeDefinition, table string) error {
	return querycore.ValidateScopes(scopes, table)
}

func checkRemovals(scopes []scopeDefinition, removed []scopeNameData) error {
	return querycore.CheckRemovals(scopes, removed)
}

func contextScopeResolver(scope scopeDefinition, table string) func(context.Context) (expression, error) {
	return querycore.ContextScopeResolver(scope, table)
}

type scopeNode = querycore.ScopeNode

type scopeDefinition = querycore.ScopeDefinition

// GlobalScope is a named default predicate a model declares once, next to the
// model, through a define<Model>GlobalScopes function returning its scopes. Every query
// of the model applies it through the same effective-predicate owner as soft
// deletion: reads, pagination, chunking, counts, relations and WhereHas,
// relation aggregates, joins and subqueries built from the model query, and
// updates/deletes (including source writes). Inserts are not filtered.
//
// Keep the value in a package variable to opt out per query with
// WithoutGlobalScope. Scopes are immutable; there is no mutable registry.
type GlobalScope[M any] struct {
	_               [0]*M
	scopeDefinition scopeDefinition
}

// NewGlobalScope declares a scope with a fixed typed predicate.
func NewGlobalScope[M any](name GlobalScopeName, predicate Predicate[M]) GlobalScope[M] {
	return GlobalScope[M]{scopeDefinition: scopeDefinition{Name: scopeNameData(name), Predicate: predicateExpression(predicate.expression)}}
}

// NewContextScope declares a scope whose predicate depends on the executing
// request, such as a tenant. resolve must read typed values from the supplied
// context explicitly (for example tenant.From(ctx)) and return an error when
// they are absent; it must not return an always-true predicate as a fallback.
// Execution methods resolve it with their own context on every call, so a
// reused query never keeps another request's scope. Compile without execution
// requires WithScopeContext; an unresolved context scope fails closed.
func NewContextScope[M any](name GlobalScopeName, resolve func(context.Context) (Predicate[M], error)) GlobalScope[M] {
	var lowered func(context.Context) (expression, error)
	if resolve != nil {
		lowered = func(ctx context.Context) (expression, error) {
			p, err := resolve(ctx)
			return predicateExpression(p.expression), err
		}
	}
	return GlobalScope[M]{scopeDefinition: scopeDefinition{Name: scopeNameData(name), Resolve: lowered}}
}

// Name reports the scope's declared name.
func (s GlobalScope[M]) Name() GlobalScopeName { return GlobalScopeName(s.scopeDefinition.Name) }

// WithGlobalScopes attaches scopes known when the definition is built.
func (d Definition[M]) WithGlobalScopes(scopes ...GlobalScope[M]) Definition[M] {
	d.modelDefinition.GlobalScopes = lowerScopes(scopes)
	return d
}

// WithGlobalScopeSource attaches the model's define<Model>GlobalScopes function, which
// generated code passes without calling. The source runs once, on the first
// compilation that needs it, after the model's generated query exists; scopes
// may therefore use the model's own relations, such as Owner.Exists(). It must
// not compile or execute queries itself.
func (d Definition[M]) WithGlobalScopeSource(source func() []GlobalScope[M]) Definition[M] {
	d.modelDefinition.ScopeSource = &lazyScopes{}
	if source != nil {
		d.modelDefinition.ScopeSource.Source = func() []scopeDefinition { return lowerScopes(source()) }
	}
	return d
}

// WithoutGlobalScope removes the named model scopes from this query only.
// Explicit predicates, soft-delete visibility and related models' own scopes
// are unchanged. Removing a scope the model does not declare fails validation.
func (q Query[M]) WithoutGlobalScope(scopes ...GlobalScope[M]) Query[M] {
	excluded := slices.Clone(q.queryCore.WithoutScopes)
	for _, scope := range scopes {
		excluded = append(excluded, scope.scopeDefinition.Name)
	}
	q.queryCore.WithoutScopes = excluded
	return q
}

// WithoutGlobalScopes removes every model-declared scope from this query only.
func (q Query[M]) WithoutGlobalScopes() Query[M] { q.queryCore.AllScopesOff = true; return q }

// WithScopeContext resolves context scopes for Compile and plan inspection.
// Execution methods always rebind to their own context, so this never carries
// one request's tenant into another request's execution.
func (q Query[M]) WithScopeContext(ctx context.Context) Query[M] {
	q.queryCore.ScopeContext = ctx
	return q
}

// inContext binds context scopes to the executing call's context.
func (q Query[M]) inContext(ctx context.Context) Query[M] { q.queryCore.ScopeContext = ctx; return q }

func lowerScopes[M any](scopes []GlobalScope[M]) []scopeDefinition {
	result := make([]scopeDefinition, len(scopes))
	for i, scope := range scopes {
		result[i] = scope.scopeDefinition
	}
	return result
}
