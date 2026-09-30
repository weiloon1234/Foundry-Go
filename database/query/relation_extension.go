package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
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
type ExtensionSlot[M any] struct{ spec *extensionSlotSpec[M] }

type extensionSlotSpec[M any] struct {
	name, table string
	err         error
	load        func(context.Context, database.Executor, []M, *relationLoadState, bool) ([]M, error)
}

// NewExtensionSlot binds generated slot accessors to an external fetch.
func NewExtensionSlot[M, S any](binding ExtensionBinding[M, S]) ExtensionSlot[M] {
	spec := &extensionSlotSpec[M]{name: binding.Name, table: binding.Table}
	switch {
	case binding.Name == "" || binding.Table == "" || binding.Get == nil || binding.Set == nil || binding.Loaded == nil || binding.Count == nil:
		spec.err = fault.New(fault.Invalid, "extension slot requires a generated binding")
	case binding.Fetch == nil:
		spec.err = binding.Unbound
		if spec.err == nil {
			spec.err = fault.New(fault.Missing, "extension slot is not bound to a runtime")
		}
	}
	fetch := binding.Fetch
	spec.load = func(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, missing bool) ([]M, error) {
		selected, indices := selectedParents(parents, func(m M) bool { return missing && binding.Loaded(binding.Get(m)) })
		if len(selected) == 0 {
			return parents, nil
		}
		// Parent batches follow RelationLimits.BatchSize, as key batches do
		// for ordinary relations; a store may split a batch further. Each
		// batch charges the shared budget before the next one is fetched, so
		// an over-budget load stops early instead of retaining every batch.
		values := make([]S, 0, len(selected))
		for start := 0; start < len(selected); start += state.limits.BatchSize {
			batch := selected[start:min(start+state.limits.BatchSize, len(selected))]
			fetched, err := fetch(ctx, executor, batch)
			if err != nil {
				return nil, err
			}
			if len(fetched) != len(batch) {
				return nil, fault.New(fault.Invalid, "extension slot fetch returned a different number of values")
			}
			count := 0
			for _, v := range fetched {
				count += binding.Count(v)
			}
			if count > state.remaining {
				return nil, fault.New(fault.Invalid, "related rows exceed the shared loading budget")
			}
			state.remaining -= count
			if err := state.attach(count); err != nil {
				return nil, err
			}
			values = append(values, fetched...)
		}
		result := parents // owned by loadRelations, which copied the caller slice once
		for i, v := range values {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result[indices[i]] = binding.Set(result[indices[i]], v)
		}
		return result, nil
	}
	return ExtensionSlot[M]{spec: spec}
}

func (r ExtensionSlot[M]) relationName() string {
	if r.spec == nil {
		return ""
	}
	return r.spec.name
}
func (r ExtensionSlot[M]) copyRelation() Relation[M] { return r }
func (r ExtensionSlot[M]) validateRelation(table string, depth int, limits RelationLimits) error {
	if r.spec == nil {
		return fault.New(fault.Invalid, "extension slot requires a generated descriptor")
	}
	if r.spec.err != nil {
		return r.spec.err
	}
	if depth > limits.MaxDepth {
		return fault.New(fault.Invalid, "relation nesting exceeds its depth bound")
	}
	if table != r.spec.table {
		return fault.New(fault.Invalid, "extension slot belongs to a different model table")
	}
	return nil
}
func (r ExtensionSlot[M]) loadRelation(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, _ int, missing bool) ([]M, error) {
	if r.spec == nil || r.spec.err != nil || r.spec.load == nil {
		return nil, fault.New(fault.Invalid, "extension slot requires a bound descriptor")
	}
	return r.spec.load(ctx, executor, parents, state, missing)
}
