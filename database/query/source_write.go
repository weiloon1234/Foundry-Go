package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// UpdateSource performs an explicit set-based UPDATE FROM. SQL expressions use
// stored field values; literal inputs retain normal field mutators and clocks.
// Per-model write and retrieval observers are skipped. Mapped values require at
// most one source row per affected model; ambiguity rolls back the entire write.
type UpdateSource[S, M any] struct{ plan sourceMutation[S, M] }

// DeleteSource performs an explicit set-based DELETE USING, or a soft-delete
// update when configured. Duplicate source keys affect each destination once.
// It skips per-model write and retrieval observers.
type DeleteSource[S, M any] struct{ plan sourceMutation[S, M] }

type sourceMutation[S, M any] struct {
	source      projectionSource[S]
	destination Query[M]
	key         SourceKey[S, M]
	mappings    []UpdateMapping[S, M]
	values      Mutation[M]
	kind        mutationKind
}

func newSourceMutation[S, M any](destination ModelQuerySource[M], source ProjectionSource[S], kind mutationKind) sourceMutation[S, M] {
	p := sourceMutation[S, M]{kind: kind}
	if nilDescriptor(destination) || nilDescriptor(source) {
		p.source.err = fault.New(fault.Invalid, "source write requires a destination and source query")
		return p
	}
	p.destination = destination.modelQuery()
	p.source = source.projectionSource()
	return p
}

// UpdateFrom preserves destination filters and source windows independently.
// A generated Update<Model>From builder supplies model-specific selectors.
func UpdateFrom[S, M any](destination ModelQuerySource[M], source ProjectionSource[S]) UpdateSource[S, M] {
	return UpdateSource[S, M]{plan: newSourceMutation(destination, source, updateModel)}
}

// DeleteUsing applies ordinary model removal visibility to the destination.
// Source NULL keys match nothing. Use ForceDeleteUsing for physical soft-model removal.
func DeleteUsing[S, M any](destination ModelQuerySource[M], source ProjectionSource[S]) DeleteSource[S, M] {
	p := newSourceMutation(destination, source, deleteModel)
	p.destination, p.kind = p.destination.removal()
	return DeleteSource[S, M]{plan: p}
}

// ForceDeleteUsing physically deletes a configured soft-delete model, retaining
// explicit destination visibility. WithTrashed includes previously deleted rows.
func ForceDeleteUsing[S, M any](destination ModelQuerySource[M], source ProjectionSource[S]) DeleteSource[S, M] {
	return DeleteSource[S, M]{plan: newSourceMutation(destination, source, forceDeleteModel)}
}

func (UpdateSource[S, M]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model update from query"))
}
func (DeleteSource[S, M]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model delete using query"))
}

// Match replaces the required source-to-primary identity mapping.
func (p UpdateSource[S, M]) Match(key SourceKey[S, M]) UpdateSource[S, M] { p.plan.key = key; return p }

// Match replaces the required source-to-primary identity mapping.
func (p DeleteSource[S, M]) Match(key SourceKey[S, M]) DeleteSource[S, M] { p.plan.key = key; return p }

// Select appends source assignments. Primary, mutated and managed update fields
// cannot be selected; duplicate mappings or overlap with Values are invalid.
func (p UpdateSource[S, M]) Select(mappings ...UpdateMapping[S, M]) UpdateSource[S, M] {
	p.plan.mappings = append(slices.Clone(p.plan.mappings), mappings...)
	return p
}

// Values replaces fixed draft inputs, normalized once inside the transaction.
// Multiple source matches are permitted when there are no SQL value mappings.
func (p UpdateSource[S, M]) Values(values Mutation[M]) UpdateSource[S, M] {
	p.plan.values = Change(values.assignments...)
	return p
}

// Exec returns an affected count without collecting models. It never truncates
// the source window and rolls back ambiguous SQL-mapped updates.
func (p UpdateSource[S, M]) Exec(ctx context.Context, writer database.Transactor) (int64, error) {
	return p.plan.exec(ctx, writer)
}

// Exec returns the number of affected destination models, without collecting them.
func (p DeleteSource[S, M]) Exec(ctx context.Context, writer database.Transactor) (int64, error) {
	return p.plan.exec(ctx, writer)
}

// Returning retains at most limit complete models (1..MaxInsertRows). Excess
// returned rows roll back. The bound does not limit SQL work or field sizes,
// does not truncate source selection, and implies no result ordering.
func (p UpdateSource[S, M]) Returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return p.plan.returning(ctx, writer, limit)
}

// Returning retains bounded complete affected models with the same limits and
// rollback behavior as UpdateSource.Returning. Physical deletion returns old values.
func (p DeleteSource[S, M]) Returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return p.plan.returning(ctx, writer, limit)
}
