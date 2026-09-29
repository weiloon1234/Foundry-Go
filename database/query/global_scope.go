package query

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// GlobalScopeName identifies one model-declared default scope. It uses the SQL
// identifier grammar so diagnostics never echo arbitrary text.
type GlobalScopeName string

// GlobalScope is a named default predicate a model declares once, next to the
// model, through a DefineGlobalScopes method returning its scopes. Every query
// of the model applies it through the same effective-predicate owner as soft
// deletion: reads, pagination, chunking, counts, relations and WhereHas,
// relation aggregates, joins and subqueries built from the model query, and
// updates/deletes (including source writes). Inserts are not filtered.
//
// Keep the value in a package variable to opt out per query with
// WithoutGlobalScope. Scopes are immutable; there is no mutable registry.
type GlobalScope[M any] struct {
	name      GlobalScopeName
	predicate Predicate[M]
	resolve   func(context.Context) (Predicate[M], error)
}

// NewGlobalScope declares a scope with a fixed typed predicate.
func NewGlobalScope[M any](name GlobalScopeName, predicate Predicate[M]) GlobalScope[M] {
	return GlobalScope[M]{name: name, predicate: predicate}
}

// NewContextScope declares a scope whose predicate depends on the executing
// request, such as a tenant. resolve must read typed values from the supplied
// context explicitly (for example tenant.From(ctx)) and return an error when
// they are absent; it must not return an always-true predicate as a fallback.
// Execution methods resolve it with their own context on every call, so a
// reused query never keeps another request's scope. Compile without execution
// requires WithScopeContext; an unresolved context scope fails closed.
func NewContextScope[M any](name GlobalScopeName, resolve func(context.Context) (Predicate[M], error)) GlobalScope[M] {
	return GlobalScope[M]{name: name, resolve: resolve}
}

// Name reports the scope's declared name.
func (s GlobalScope[M]) Name() GlobalScopeName { return s.name }

// WithGlobalScopes attaches scopes known when the definition is built.
func (d Definition[M]) WithGlobalScopes(scopes ...GlobalScope[M]) Definition[M] {
	d.globalScopes = slices.Clone(scopes)
	return d
}

// WithGlobalScopeSource attaches the model's DefineGlobalScopes method, which
// generated code passes without calling. The source runs once, on the first
// compilation that needs it, after the model's generated query exists; scopes
// may therefore use the model's own relations, such as Owner.Exists(). It must
// not compile or execute queries itself.
func (d Definition[M]) WithGlobalScopeSource(source func() []GlobalScope[M]) Definition[M] {
	d.scopeSource = &lazyScopes[M]{source: source}
	return d
}

// lazyScopes evaluates a scope source once. Until it has run, queries lower
// the model's scopes as one deferred node that the compiler expands, so
// declaring the scopes (which may lower relation predicates of this same model)
// never re-enters an evaluation in progress.
type lazyScopes[M any] struct {
	source func() []GlobalScope[M]
	once   sync.Once
	ready  atomic.Bool
	scopes []GlobalScope[M]
	err    error
}

func (l *lazyScopes[M]) get(table string) ([]GlobalScope[M], error) {
	l.once.Do(func() {
		defer l.ready.Store(true)
		if l.source == nil {
			l.err = fault.New(fault.Invalid, "global scope source is nil")
			return
		}
		var scopes []GlobalScope[M]
		if failed := callback.Invoke("define global scopes", func() error { scopes = slices.Clone(l.source()); return nil }); failed != nil {
			l.err = failed
			return
		}
		l.scopes, l.err = scopes, validateScopes(scopes, table)
	})
	return l.scopes, l.err
}

func (d Definition[M]) validateGlobalScopes() error {
	return validateScopes(d.globalScopes, d.table)
}

func validateScopes[M any](scopes []GlobalScope[M], table string) error {
	seen := make(map[GlobalScopeName]bool, len(scopes))
	for _, scope := range scopes {
		if !sqlname.Valid(string(scope.name)) || seen[scope.name] || (scope.resolve == nil) == (scope.predicate.expression == nil) {
			return fault.New(fault.Invalid, "global scopes require unique identifier names and exactly one predicate source")
		}
		seen[scope.name] = true
		if scope.predicate.expression != nil {
			nodes := 0
			if err := validateExpression(scope.predicate.expression, table, 0, &nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

// WithoutGlobalScope removes the named model scopes from this query only.
// Explicit predicates, soft-delete visibility and related models' own scopes
// are unchanged. Removing a scope the model does not declare fails validation.
func (q Query[M]) WithoutGlobalScope(scopes ...GlobalScope[M]) Query[M] {
	excluded := slices.Clone(q.withoutScopes)
	for _, scope := range scopes {
		excluded = append(excluded, scope.name)
	}
	q.withoutScopes = excluded
	return q
}

// WithoutGlobalScopes removes every model-declared scope from this query only.
func (q Query[M]) WithoutGlobalScopes() Query[M] { q.allScopesOff = true; return q }

// WithScopeContext resolves context scopes for Compile and plan inspection.
// Execution methods always rebind to their own context, so this never carries
// one request's tenant into another request's execution.
func (q Query[M]) WithScopeContext(ctx context.Context) Query[M] { q.scopeContext = ctx; return q }

// inContext binds context scopes to the executing call's context.
func (q Query[M]) inContext(ctx context.Context) Query[M] { q.scopeContext = ctx; return q }

func (q Query[M]) validateScopeOptions() error {
	if len(q.withoutScopes) == 0 {
		return nil
	}
	if q.definition == nil {
		return fault.New(fault.Invalid, "global scope removal requires model metadata")
	}
	if q.definition.scopeSource != nil {
		// Checked when the deferred scopes are expanded.
		return nil
	}
	return checkRemovals(q.definition.globalScopes, q.withoutScopes)
}

func checkRemovals[M any](scopes []GlobalScope[M], removed []GlobalScopeName) error {
	for _, name := range removed {
		if !slices.ContainsFunc(scopes, func(s GlobalScope[M]) bool { return s.name == name }) {
			return fault.New(fault.Invalid, "global scope removal names an undeclared scope")
		}
	}
	return nil
}

// scopePredicates returns the active scopes' predicates. Context scopes become
// placeholders that the compiler resolves with the bound execution context.
// A model whose scope source has not run yet contributes one deferred node.
func (q Query[M]) scopePredicates() []expression {
	if q.definition == nil || q.allScopesOff {
		return nil
	}
	lazy := q.definition.scopeSource
	if lazy == nil {
		return q.activeScopePredicates(q.definition.globalScopes)
	}
	if !lazy.ready.Load() {
		return []expression{q.deferredScopes(lazy)}
	}
	// Once evaluated, compile the same single conjunction the deferred node
	// expands to, so statement text (and cursor fingerprints) never change.
	evaluated, err := lazy.get(q.table)
	scopes := append(slices.Clone(q.definition.globalScopes), evaluated...)
	if err == nil {
		err = checkRemovals(scopes, q.withoutScopes)
	}
	if err != nil {
		return []expression{scopeNode{name: "declared", resolve: func(context.Context) (expression, error) { return nil, err }}}
	}
	return []expression{junction{children: q.activeScopePredicates(scopes)}}
}

func (q Query[M]) activeScopePredicates(scopes []GlobalScope[M]) []expression {
	result := make([]expression, 0, len(scopes))
	for _, scope := range scopes {
		if slices.Contains(q.withoutScopes, scope.name) {
			continue
		}
		if scope.resolve == nil {
			result = append(result, scope.predicate.expression)
			continue
		}
		result = append(result, scopeNode{name: scope.name, context: q.scopeContext, needsContext: true, resolve: contextScopeResolver(scope, q.table)})
	}
	return result
}

// deferredScopes expands the model's scopes at compilation: it runs the
// source once, checks removals, and resolves context scopes with the
// compiler's context (an absent context fails only if a context scope exists).
func (q Query[M]) deferredScopes(lazy *lazyScopes[M]) scopeNode {
	eager, removed, table, bound := q.definition.globalScopes, q.withoutScopes, q.table, q.scopeContext
	return scopeNode{name: "declared", context: bound, resolve: func(ctx context.Context) (expression, error) {
		evaluated, err := lazy.get(table)
		if err != nil {
			return nil, err
		}
		scopes := append(slices.Clone(eager), evaluated...)
		if err := checkRemovals(scopes, removed); err != nil {
			return nil, err
		}
		active := q.activeScopePredicates(scopes)
		for i, predicate := range active {
			node, deferred := predicate.(scopeNode)
			if !deferred {
				continue
			}
			if ctx == nil {
				ctx = node.context
			}
			if ctx == nil {
				return nil, fault.New(fault.Invalid, "global scope "+string(node.name)+" reads the request context; execute the query or bind it with WithScopeContext")
			}
			resolved, err := node.resolve(ctx)
			if err != nil {
				return nil, err
			}
			active[i] = resolved
		}
		return junction{children: active}, nil
	}}
}

// contextScopeResolver contains application panics and validates the resolved
// predicate against the model table before any requalification wraps it.
func contextScopeResolver[M any](scope GlobalScope[M], table string) func(context.Context) (expression, error) {
	return func(ctx context.Context) (expression, error) {
		var predicate Predicate[M]
		err := callback.Invoke("global scope resolution", func() error {
			var err error
			predicate, err = scope.resolve(ctx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if predicate.expression == nil {
			return nil, fault.New(fault.Invalid, "global scope resolved to an empty predicate")
		}
		nodes := 0
		if err := validateExpression(predicate.expression, table, 0, &nodes); err != nil {
			return nil, err
		}
		return predicate.expression, nil
	}
}

// scopeNode is a context scope resolved when compiled. The executing call's
// context, supplied to the compiler, always wins, so a query value lowered into
// another statement never keeps an earlier request's scope; the context
// captured from WithScopeContext serves only Compile and plan inspection.
// Rewrites such as alias requalification compose onto resolve.
type scopeNode struct {
	name    GlobalScopeName
	context context.Context
	// needsContext marks a context scope; a deferred scope expansion resolves
	// its own context scopes and may run without a context.
	needsContext bool
	resolve      func(context.Context) (expression, error)
}

func (scopeNode) expressionNode() {}

func (n scopeNode) resolved(executing context.Context) (expression, error) {
	if n.resolve == nil {
		return nil, fault.New(fault.Invalid, "invalid global scope")
	}
	ctx := executing
	if ctx == nil {
		ctx = n.context
	}
	if ctx == nil && n.needsContext {
		return nil, fault.New(fault.Invalid, "global scope "+string(n.name)+" reads the request context; execute the query or bind it with WithScopeContext")
	}
	return n.resolve(ctx)
}

// rewrite composes an expression rewrite onto the deferred resolution.
func (n scopeNode) rewrite(fn func(expression) (expression, error)) scopeNode {
	resolve := n.resolve
	n.resolve = func(ctx context.Context) (expression, error) {
		e, err := resolve(ctx)
		if err != nil {
			return nil, err
		}
		return fn(e)
	}
	return n
}
