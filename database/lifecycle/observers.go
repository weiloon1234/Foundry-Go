package lifecycle

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Observer identifies one model-owned hook factory. M and H retain the concrete
// model and its hook declaration at the adapter boundary. Names are
// semantic identifiers, unique within a database's observer set.
type Observer[M, H any] struct {
	name string
	kind observerKind
	_    [0]*M
	_    [0]*H
}

type observerKind uint8

const (
	writeObserver observerKind = iota
	retrievalObserver
	deletionObserver
)

func (k observerKind) valid() bool {
	return k == writeObserver || k == retrievalObserver || k == deletionObserver
}

// NewObserver declares a factory for a model's normal write pipeline.
func NewObserver[M, H any](name string) Observer[M, H] { return Observer[M, H]{name: name} }

// NewRetrievalObserver declares a separate factory for complete-model reads.
// It shares database registration and naming rules with write observers but is
// excluded from write lookup. Declaration and inspection never invoke factories.
func NewRetrievalObserver[M, H any](name string) Observer[M, H] {
	return Observer[M, H]{name: name, kind: retrievalObserver}
}

// NewDeletionObserver declares a write factory that observes only deletions:
// delete, soft delete and force delete. Other writes neither construct it nor
// take the hooked path because of it, and set-based writes other than
// deletions need no WithoutModelHooks for it. Its hooks share the model's
// write hook type; only their deletion callbacks run.
func NewDeletionObserver[M, H any](name string) Observer[M, H] {
	return Observer[M, H]{name: name, kind: deletionObserver}
}

func (o Observer[M, H]) Name() string { return o.name }

// Validate checks the declaration without constructing operation-local hooks.
func (o Observer[M, H]) Validate() error {
	if !identifier.Semantic(o.name) || !o.kind.valid() {
		return fault.New(fault.Invalid, "invalid model observer identifier")
	}
	return nil
}

// Declaration holds an opaque, typed model/factory pair for database assembly.
// Construct declarations through Observer.Declare; the zero value is invalid.
type Declaration struct {
	name    string
	model   reflect.Type
	hooks   reflect.Type
	factory any
	kind    observerKind
}

// Declare retains a factory without invoking it. The factory creates fresh
// operation-local hooks at dispatch; captured application services must be safe
// for concurrent operations. A declaration is immutable after construction.
func (o Observer[M, H]) Declare(factory func() H) (Declaration, error) {
	if err := o.Validate(); err != nil {
		return Declaration{}, err
	}
	if factory == nil {
		return Declaration{}, fault.New(fault.Invalid, "model observer needs a hook factory")
	}
	return Declaration{name: o.name, model: reflect.TypeFor[M](), hooks: reflect.TypeFor[H](), factory: factory, kind: o.kind}, nil
}

type observerGroupKey struct {
	model reflect.Type
	kind  observerKind
}

type observerGroup struct {
	hooks     reflect.Type
	factories []any
}

// Observers is an immutable set of explicitly declared factories, grouped by
// exact model type and write/retrieval/deletion kind, retaining declaration
// order within each group. Its zero value is empty.
// It holds no mutable registration API or process-global application state.
type Observers struct {
	groups map[observerGroupKey]observerGroup
	// deletions is the view deletion writes dispatch: each model's write
	// group joined by its deletion observers, in declaration order.
	deletions *Observers
}

// NewObservers validates the complete set before publication. It neither runs
// hook factories nor retains the caller's declaration slice. Duplicate names
// and conflicting hook types for one model/kind fail without a partial result.
// Write and retrieval factories for the same model may have different types.
func NewObservers(declarations ...Declaration) (Observers, error) {
	set := Observers{groups: make(map[observerGroupKey]observerGroup)}
	deletions := Observers{groups: make(map[observerGroupKey]observerGroup)}
	names := make(map[string]struct{}, len(declarations))
	for _, item := range declarations {
		if !identifier.Semantic(item.name) || item.model == nil || item.hooks == nil || item.factory == nil || !item.kind.valid() {
			return Observers{}, fault.New(fault.Invalid, "invalid model observer declaration")
		}
		if _, exists := names[item.name]; exists {
			return Observers{}, fault.New(fault.Duplicate, "duplicate model observer identifier")
		}
		names[item.name] = struct{}{}
		key := observerGroupKey{model: item.model, kind: item.kind}
		group, exists := set.groups[key]
		if exists && group.hooks != item.hooks {
			return Observers{}, fault.New(fault.Invalid, "model observers have incompatible hook declarations")
		}
		group.hooks = item.hooks
		group.factories = append(group.factories, item.factory)
		set.groups[key] = group
		// Deletions dispatch write and deletion observers as one write group,
		// so both kinds of one model must share its generated hook type.
		view := key
		if item.kind == deletionObserver {
			view.kind = writeObserver
		}
		merged, exists := deletions.groups[view]
		if exists && merged.hooks != item.hooks {
			return Observers{}, fault.New(fault.Invalid, "model observers have incompatible hook declarations")
		}
		merged.hooks = item.hooks
		merged.factories = append(merged.factories, item.factory)
		deletions.groups[view] = merged
	}
	set.deletions = &deletions
	return set, nil
}

// ForDeletion is the set a deletion dispatches: write observers joined by
// deletion observers. Other writes use the set itself, which excludes them.
func (s Observers) ForDeletion() Observers {
	if s.deletions == nil {
		return s
	}
	return *s.deletions
}

// HasObservers reports write registrations for exactly M without invoking
// factories. A retrieval-only model does not enter the write-hook pipeline.
func HasObservers[M any](set Observers) bool {
	return hasObservers[M](set, writeObserver)
}

// HasDeletionObservers reports registrations that observe deletions of exactly
// M: write observers and deletion observers.
func HasDeletionObservers[M any](set Observers) bool {
	return hasObservers[M](set, writeObserver) || hasObservers[M](set, deletionObserver)
}

// HasRetrievalObservers reports retrieval registrations for exactly M without
// constructing either retrieval or write hooks.
func HasRetrievalObservers[M any](set Observers) bool {
	return hasObservers[M](set, retrievalObserver)
}

func hasObservers[M any](set Observers, kind observerKind) bool {
	return len(set.groups[observerGroupKey{model: reflect.TypeFor[M](), kind: kind}].factories) != 0
}

// ObserverFactories supplies independent typed write factories to an adapter.
// Callers invoke factories inside the owning operation, never during metadata
// inspection. Requesting an incompatible hook type fails before any callback.
func ObserverFactories[M, H any](set Observers) ([]func() H, error) {
	return observerFactories[M, H](set, writeObserver)
}

// RetrievalObserverFactories supplies only typed retrieval factories. It never
// instantiates or returns write factories, including for a model with both kinds.
// The returned slice is independent of the immutable registry.
func RetrievalObserverFactories[M, H any](set Observers) ([]func() H, error) {
	return observerFactories[M, H](set, retrievalObserver)
}

func observerFactories[M, H any](set Observers, kind observerKind) ([]func() H, error) {
	group, exists := set.groups[observerGroupKey{model: reflect.TypeFor[M](), kind: kind}]
	if exists && group.hooks != reflect.TypeFor[H]() {
		return nil, fault.New(fault.Invalid, "model observer hook type does not match the generated adapter")
	}
	result := make([]func() H, len(group.factories))
	for i, stored := range group.factories {
		factory, ok := stored.(func() H)
		if !ok || factory == nil {
			return nil, fault.New(fault.Internal, "model observer factory does not match its declaration")
		}
		result[i] = factory
	}
	return result, nil
}
