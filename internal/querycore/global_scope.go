package querycore

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

// lazyScopes evaluates a scope source once. Until it has run, queries lower
// the model's scopes as one deferred node that the compiler expands, so
// declaring the scopes (which may lower relation predicates of this same model)
// never re-enters an evaluation in progress.
type LazyScopes struct {
	Source func() []ScopeDefinition
	Once   sync.Once
	Ready  atomic.Bool
	Scopes []ScopeDefinition
	Err    error
}

func (l *LazyScopes) Get(table string) ([]ScopeDefinition, error) {
	l.Once.Do(func() {
		defer l.Ready.Store(true)
		if l.Source == nil {
			l.Err = fault.New(fault.Invalid, "global scope source is nil")
			return
		}
		var scopes []ScopeDefinition
		if failed := callback.Invoke("define global scopes", func() error { scopes = slices.Clone(l.Source()); return nil }); failed != nil {
			l.Err = failed
			return
		}
		l.Scopes, l.Err = scopes, ValidateScopes(scopes, table)
	})
	return l.Scopes, l.Err
}

func (d ModelDefinition) ValidateGlobalScopes() error {
	return ValidateScopes(d.GlobalScopes, d.Table)
}

func ValidateScopes(scopes []ScopeDefinition, table string) error {
	seen := make(map[GlobalScopeName]bool, len(scopes))
	for _, scope := range scopes {
		if !sqlname.Valid(string(scope.Name)) || seen[scope.Name] || (scope.Resolve == nil) == (scope.Predicate == nil) {
			return fault.New(fault.Invalid, "global scopes require unique identifier names and exactly one predicate source")
		}
		seen[scope.Name] = true
		if scope.Predicate != nil {
			nodes := 0
			if err := ValidateExpression(scope.Predicate, table, 0, &nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (q QueryCore) ValidateScopeOptions() error {
	if len(q.WithoutScopes) == 0 {
		return nil
	}
	if q.Model == nil {
		return fault.New(fault.Invalid, "global scope removal requires model metadata")
	}
	if q.Model.ScopeSource != nil {
		// Checked when the deferred scopes are expanded.
		return nil
	}
	return CheckRemovals(q.Model.GlobalScopes, q.WithoutScopes)
}

func CheckRemovals(scopes []ScopeDefinition, removed []GlobalScopeName) error {
	for _, name := range removed {
		if !slices.ContainsFunc(scopes, func(s ScopeDefinition) bool { return s.Name == name }) {
			return fault.New(fault.Invalid, "global scope removal names an undeclared scope")
		}
	}
	return nil
}

// scopePredicates returns the active scopes' predicates. Context scopes become
// placeholders that the compiler resolves with the bound execution context.
// A model whose scope source has not run yet contributes one deferred node.
func (q QueryCore) ScopePredicates() []Expression {
	if q.Model == nil || q.AllScopesOff {
		return nil
	}
	lazy := q.Model.ScopeSource
	if lazy == nil {
		return q.ActiveScopePredicates(q.Model.GlobalScopes)
	}
	if !lazy.Ready.Load() {
		return []Expression{q.DeferredScopes(lazy)}
	}
	// Once evaluated, compile the same single conjunction the deferred node
	// expands to, so statement text (and cursor fingerprints) never change.
	evaluated, err := lazy.Get(q.Table)
	scopes := append(slices.Clone(q.Model.GlobalScopes), evaluated...)
	if err == nil {
		err = CheckRemovals(scopes, q.WithoutScopes)
	}
	if err != nil {
		return []Expression{ScopeNode{Name: "declared", Resolve: func(context.Context) (Expression, error) { return nil, err }}}
	}
	return []Expression{Junction{Children: q.ActiveScopePredicates(scopes)}}
}

func (q QueryCore) ActiveScopePredicates(scopes []ScopeDefinition) []Expression {
	result := make([]Expression, 0, len(scopes))
	for _, scope := range scopes {
		if slices.Contains(q.WithoutScopes, scope.Name) {
			continue
		}
		if scope.Resolve == nil {
			result = append(result, scope.Predicate)
			continue
		}
		result = append(result, ScopeNode{Name: scope.Name, Context: q.ScopeContext, NeedsContext: true, Resolve: ContextScopeResolver(scope, q.Table)})
	}
	return result
}

// deferredScopes expands the model's scopes at compilation: it runs the
// source once, checks removals, and resolves context scopes with the
// compiler's context (an absent context fails only if a context scope exists).
func (q QueryCore) DeferredScopes(lazy *LazyScopes) ScopeNode {
	eager, removed, table, bound := q.Model.GlobalScopes, q.WithoutScopes, q.Table, q.ScopeContext
	return ScopeNode{Name: "declared", Context: bound, Resolve: func(ctx context.Context) (Expression, error) {
		evaluated, err := lazy.Get(table)
		if err != nil {
			return nil, err
		}
		scopes := append(slices.Clone(eager), evaluated...)
		if err := CheckRemovals(scopes, removed); err != nil {
			return nil, err
		}
		active := q.ActiveScopePredicates(scopes)
		for i, predicate := range active {
			node, deferred := predicate.(ScopeNode)
			if !deferred {
				continue
			}
			if ctx == nil {
				ctx = node.Context
			}
			if ctx == nil {
				return nil, fault.New(fault.Invalid, "global scope "+string(node.Name)+" reads the request context; execute the query or bind it with WithScopeContext")
			}
			resolved, err := node.Resolve(ctx)
			if err != nil {
				return nil, err
			}
			active[i] = resolved
		}
		return Junction{Children: active}, nil
	}}
}

// contextScopeResolver contains application panics and validates the resolved
// predicate against the model table before any requalification wraps it.
func ContextScopeResolver(scope ScopeDefinition, table string) func(context.Context) (Expression, error) {
	return func(ctx context.Context) (Expression, error) {
		var predicate Expression
		err := callback.Invoke("global scope resolution", func() error {
			var err error
			predicate, err = scope.Resolve(ctx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if predicate == nil {
			return nil, fault.New(fault.Invalid, "global scope resolved to an empty predicate")
		}
		nodes := 0
		if err := ValidateExpression(predicate, table, 0, &nodes); err != nil {
			return nil, err
		}
		return predicate, nil
	}
}

// scopeNode is a context scope resolved when compiled. The executing call's
// context, supplied to the compiler, always wins, so a query value lowered into
// another statement never keeps an earlier request's scope; the context
// captured from WithScopeContext serves only Compile and plan inspection.
// Rewrites such as alias requalification compose onto resolve.
type ScopeNode struct {
	Name    GlobalScopeName
	Context context.Context
	// needsContext marks a context scope; a deferred scope expansion resolves
	// its own context scopes and may run without a context.
	NeedsContext bool
	Resolve      func(context.Context) (Expression, error)
}

func (ScopeNode) ExpressionNode() {}

func (n ScopeNode) Resolved(executing context.Context) (Expression, error) {
	if n.Resolve == nil {
		return nil, fault.New(fault.Invalid, "invalid global scope")
	}
	ctx := executing
	if ctx == nil {
		ctx = n.Context
	}
	if ctx == nil && n.NeedsContext {
		return nil, fault.New(fault.Invalid, "global scope "+string(n.Name)+" reads the request context; execute the query or bind it with WithScopeContext")
	}
	return n.Resolve(ctx)
}

// rewrite composes an expression rewrite onto the deferred resolution.
func (n ScopeNode) Rewrite(fn func(Expression) (Expression, error)) ScopeNode {
	resolve := n.Resolve
	n.Resolve = func(ctx context.Context) (Expression, error) {
		e, err := resolve(ctx)
		if err != nil {
			return nil, err
		}
		return fn(e)
	}
	return n
}

// scopeDefinition is private SQL data; only its declaration adapter knows M.
type ScopeDefinition struct {
	Name      GlobalScopeName
	Predicate Expression
	Resolve   func(context.Context) (Expression, error)
}
