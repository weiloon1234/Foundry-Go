package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// SlotFetch loads one slot value per parent, index-aligned, from a store other
// than the model's own table. It receives the query's executor, so a store on
// the same pool can join the caller's transaction.
type SlotFetch[M, S any] func(ctx context.Context, executor database.Executor, parents []M) ([]S, error)

// ExtensionBinding describes one externally loaded model slot. Extension
// packages build it from generated slot bindings; applications use their
// typed slot descriptors instead.
type ExtensionBinding[M, S any] struct {
	// Name is the model's slot field; Table is the model's generated table.
	Name, Table string
	Get         func(M) S
	Set         func(M, S) M
	// Loaded reports whether a slot value was filled by a loader.
	Loaded func(S) bool
	// Count is the number of loaded values, each stored text set, metadata
	// value or file, charged against RelationLimits as fetched and attached.
	Count func(S) int
	// Fetch loads the values; nil leaves the slot unbound.
	Fetch SlotFetch[M, S]
	// Unbound is reported by validation, before parent SQL, when Fetch is nil.
	Unbound error
}

// ExtensionSlot eagerly loads a model slot whose values come from a framework
// extension store rather than the model's own table. Extension slot
// descriptors embed it, so With, Load, LoadMissing, pagination, chunked
// iteration and nested relations load them alongside ordinary relations.
type ExtensionSlot[M any] struct{ core extensionslot.Slot[M] }

// NewExtensionSlot binds generated slot accessors to the shared external loader.
// Example: query.NewExtensionSlot(binding).
func NewExtensionSlot[M, S any](binding ExtensionBinding[M, S]) ExtensionSlot[M] {
	return ExtensionSlot[M]{core: extensionslot.New(extensionslot.Binding[M, S]{Name: binding.Name, Table: binding.Table, Get: binding.Get, Set: binding.Set, Loaded: binding.Loaded, Count: binding.Count, Fetch: extensionslot.Fetch[M, S](binding.Fetch), Unbound: binding.Unbound})}
}
func (r ExtensionSlot[M]) relationName() string      { name, _, _, _ := r.core.Metadata(); return name }
func (r ExtensionSlot[M]) copyRelation() Relation[M] { return r }
func (r ExtensionSlot[M]) validateRelation(table string, depth int, limits RelationLimits) error {
	_, owner, _, err := r.core.Metadata()
	if err != nil {
		return err
	}
	if depth > limits.MaxDepth {
		return fault.New(fault.Invalid, "relation nesting exceeds its depth bound")
	}
	if table != owner {
		return fault.New(fault.Invalid, "extension slot belongs to a different model table")
	}
	return nil
}
func (r ExtensionSlot[M]) loadRelation(ctx context.Context, ex database.Executor, parents []M, state *relationLoadState, _ int, missing bool) ([]M, error) {
	return r.core.Load(ctx, ex, parents, querycore.RelationLimits(state.limits), &state.remaining, &state.attachments, missing)
}
