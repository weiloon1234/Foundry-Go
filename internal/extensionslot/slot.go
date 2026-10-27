// Package extensionslot owns external-store loading shared by ORM and legacy adapters.
package extensionslot

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Fetch returns values aligned with the supplied parents. Example: Fetch[Parent,Value](fetch).
type Fetch[M, S any] func(context.Context, database.Executor, []M) ([]S, error)

// Binding captures the typed slot accessors and runtime fetch. Example: New(binding).
type Binding[M, S any] struct {
	Name, Table string
	Get         func(M) S
	Set         func(M, S) M
	Loaded      func(S) bool
	Count       func(S) int
	Fetch       Fetch[M, S]
	Unbound     error
}

// Slot retains only shared state; callbacks retain the concrete parent/value codecs.
// Example: slot := New(binding).
type Slot[M any] struct {
	_    [0]*M
	core *state
}
type state struct {
	name, table string
	valueType   reflect.Type
	err         error
	prepare     func(unsafe.Pointer, int, bool) sequence
}
type sequence struct {
	length int
	fetch  func(context.Context, database.Executor, int, int) (batch, error)
}
type batch struct {
	count int
	apply func(context.Context) error
}

// New snapshots a binding without invoking its callbacks. Example: New(binding).
func New[M, S any](binding Binding[M, S]) Slot[M] {
	s := &state{name: binding.Name, table: binding.Table, valueType: reflect.TypeFor[S]()}
	switch {
	case binding.Name == "" || binding.Table == "" || binding.Get == nil || binding.Set == nil || binding.Loaded == nil || binding.Count == nil:
		s.err = safeerror.NewFault(fault.Invalid, "extension slot requires a generated binding")
	case binding.Fetch == nil:
		s.err = binding.Unbound
		if s.err == nil {
			s.err = safeerror.NewFault(fault.Missing, "extension slot is not bound to a runtime")
		}
	}
	s.prepare = func(base unsafe.Pointer, length int, missing bool) sequence {
		parents := unsafe.Slice((*M)(base), length)
		selected := make([]M, 0, length)
		indices := make([]int, 0, length)
		for i, parent := range parents {
			if missing && binding.Loaded(binding.Get(parent)) {
				continue
			}
			selected = append(selected, parent)
			indices = append(indices, i)
		}
		return sequence{length: len(selected), fetch: func(ctx context.Context, ex database.Executor, start, end int) (batch, error) {
			fetched, err := binding.Fetch(ctx, ex, selected[start:end])
			if err != nil {
				return batch{}, err
			}
			if len(fetched) != end-start {
				return batch{}, safeerror.NewFault(fault.Invalid, "extension slot fetch returned a different number of values")
			}
			count := 0
			for _, v := range fetched {
				count += binding.Count(v)
			}
			// A fetch may reuse its result buffer on the next batch. Retain the same
			// shallow value snapshot as the original append-based loader.
			fetched = slices.Clone(fetched)
			return batch{count: count, apply: func(ctx context.Context) error {
				for i, v := range fetched {
					if err := ctx.Err(); err != nil {
						return err
					}
					parents[indices[start+i]] = binding.Set(parents[indices[start+i]], v)
				}
				return nil
			}}, nil
		}}
	}
	return Slot[M]{core: s}
}

// Metadata exposes validated slot shape without dispatch. Example: slot.Metadata().
func (s Slot[M]) Metadata() (string, string, reflect.Type, error) {
	if s.core == nil {
		return "", "", nil, safeerror.NewFault(fault.Invalid, "extension slot requires a generated descriptor")
	}
	return s.core.name, s.core.table, s.core.valueType, s.core.err
}

// Load fills an owned parent slice under the shared relation budgets.
// Example: slot.Load(ctx,ex,parents,limits,&remaining,&attached,true).
func (s Slot[M]) Load(ctx context.Context, ex database.Executor, parents []M, limits querycore.RelationLimits, remaining, attached *int, missing bool) ([]M, error) {
	if s.core == nil {
		return nil, safeerror.NewFault(fault.Invalid, "extension slot requires a bound descriptor")
	}
	if err := s.core.load(ctx, ex, unsafe.Pointer(unsafe.SliceData(parents)), len(parents), limits, remaining, attached, missing); err != nil {
		return nil, err
	}
	return parents, nil
}
func (s *state) load(ctx context.Context, ex database.Executor, parents unsafe.Pointer, length int, limits querycore.RelationLimits, remaining, attached *int, missing bool) error {
	if s.err != nil {
		return s.err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	sequence := s.prepare(parents, length, missing)
	var batches []batch
	for start := 0; start < sequence.length; start += limits.BatchSize {
		fetched, err := sequence.fetch(ctx, ex, start, min(start+limits.BatchSize, sequence.length))
		if err != nil {
			return err
		}
		if err := querycore.ConsumeRelationBudget(remaining, fetched.count, false); err != nil {
			return err
		}
		if err := querycore.ConsumeRelationBudget(attached, fetched.count, true); err != nil {
			return err
		}
		batches = append(batches, fetched)
	}
	for _, batch := range batches {
		if err := batch.apply(ctx); err != nil {
			return err
		}
	}
	return nil
}
