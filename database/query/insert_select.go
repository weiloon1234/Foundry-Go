package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// InsertSelect keeps the SELECT scope S separate from the destination model M.
// This is an explicit set-based write: it does not dispatch per-model lifecycle
// or retrieval observers. Values still use the destination's Go field mutators.
type InsertSelect[S, M any] struct {
	source      projectionSource[S]
	destination Query[M]
	mappings    []InsertMapping[S, M]
	values      Mutation[M]
}

// Format omits captured source parameters and literal draft inputs.
func (InsertSelect[S, M]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model insert from query"))
}

// InsertFrom is the generated insertion boundary. Omitted columns retain their
// database defaults or NULL; a Go-generated UUID is not shared across source rows.
// Applications normally use the generated Insert<Model>From function.
func InsertFrom[S, M any](source ProjectionSource[S], destination Query[M], mappings ...InsertMapping[S, M]) InsertSelect[S, M] {
	p := InsertSelect[S, M]{destination: destination, mappings: slices.Clone(mappings)}
	if nilDescriptor(source) {
		p.source.err = fault.New(fault.Invalid, "insert from query requires a source")
	} else {
		p.source = source.projectionSource()
	}
	return p
}

// Select appends typed stored-value mappings. A field cannot also appear in
// Values, repeat, bypass a Go mutator, or replace a managed update timestamp.
func (p InsertSelect[S, M]) Select(mappings ...InsertMapping[S, M]) InsertSelect[S, M] {
	p.mappings = append(slices.Clone(p.mappings), mappings...)
	return p
}

// Values replaces the literal inputs applied to every selected row. Inputs are
// normalized once inside the owning transaction, never once per source row.
func (p InsertSelect[S, M]) Values(values Mutation[M]) InsertSelect[S, M] {
	p.values = Change(values.assignments...)
	return p
}

// Exec inserts the complete selected window and returns its affected row count
// without collecting models. Context cancellation and database constraints apply.
func (p InsertSelect[S, M]) Exec(ctx context.Context, writer database.Transactor) (int64, error) {
	p.destination = p.destination.inContext(ctx)
	if err := p.validateExecution(ctx, writer); err != nil {
		return 0, err
	}
	return executeModelStatement(ctx, writer, p.needsPreparation(), p.prepare(false), func(ctx context.Context, tx *database.Tx, statement Statement) (int64, error) {
		result, err := tx.Exec(ctx, statement.sql, statement.arguments...)
		return result.RowsAffected, err
	})
}

// Returning inserts the complete selected window and collects at most limit
// complete models (1..MaxInsertRows). Excess returned rows roll back the write.
// The bound limits retained models, not SQL work or individual field sizes;
// it never silently truncates the source. RETURNING order is not source order.
func (p InsertSelect[S, M]) Returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	p.destination = p.destination.inContext(ctx)
	if limit <= 0 || limit > MaxInsertRows {
		return nil, fault.New(fault.Invalid, "insert returning requires a positive bounded result limit")
	}
	if err := p.validateExecution(ctx, writer); err != nil {
		return nil, err
	}
	return executeModelStatement(ctx, writer, p.needsPreparation(), p.prepare(true), func(ctx context.Context, tx *database.Tx, statement Statement) ([]M, error) {
		return returningModels(ctx, tx, statement, p.destination.definition.scan, 0, limit)
	})
}

func (p InsertSelect[S, M]) needsPreparation() bool {
	return p.destination.hasFieldMutators() || p.destination.hasTimestamps()
}

func (p InsertSelect[S, M]) validateExecution(ctx context.Context, writer database.Transactor) error {
	if err := writeContext(ctx, writer); err != nil {
		return err
	}
	_, _, _, err := p.validateShape(true)
	return err
}
