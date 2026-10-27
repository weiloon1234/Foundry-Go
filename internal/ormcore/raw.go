package ormcore

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Raw validates the complete result shape before iteration, then reuses the
// caller's decoder. It deliberately neither routes reads nor applies scopes.
func Raw(ctx context.Context, ex database.Executor, columns []string, statement string, args []any, scan func(database.Row) error) error {
	return RawComplete(ctx, ex, columns, statement, args, scan, nil)
}

// RawComplete adds retrieval dispatch for complete model decoders only.
func RawComplete(ctx context.Context, ex database.Executor, columns []string, statement string, args []any, scan func(database.Row) error, callbacks *ReadCallbacks) error {
	if ctx == nil || NilDescriptor(ex) {
		return Invalid("raw execution requires a context and executor")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := ex.Query(ctx, statement, args...)
	if err != nil {
		return err
	}
	actual, err := rows.Columns()
	if err != nil {
		return safeerror.Join("operation failed", err, rows.Close())
	}
	order, err := rawColumnOrder(columns, actual)
	if err != nil {
		return safeerror.Join("operation failed", err, rows.Close())
	}
	mapped := &rawRow{Rows: rows, order: order, destinations: make([]any, len(order))}
	return Collect(ctx, rows, func(row database.Row) error {
		mapped.row = row
		return scan(mapped)
	}, callbacks)
}

// rawRow preserves the stream's FieldEncryption scope while ordering scanner
// destinations by the actual result columns instead of declaration order.
type rawRow struct {
	*database.Rows
	row          database.Row
	order        []int
	destinations []any
}

func (r *rawRow) Scan(dest ...any) error {
	if len(dest) != len(r.order) {
		return Invalid("raw decoder column count mismatch")
	}
	for i, declared := range r.order {
		r.destinations[i] = dest[declared]
	}
	return r.row.Scan(r.destinations...)
}

func rawColumnOrder(declared, actual []string) ([]int, error) {
	indices := make(map[string]int, len(declared))
	for i, name := range declared {
		indices[name] = i
	}
	seen := make(map[string]bool, len(actual))
	order := make([]int, len(actual))
	var errs []error
	for i, name := range actual {
		index, ok := indices[name]
		if !ok {
			errs = append(errs, safeerror.New(fmt.Sprintf("unexpected column %q", name)))
		}
		if seen[name] {
			errs = append(errs, safeerror.New(fmt.Sprintf("duplicate column %q", name)))
		}
		seen[name] = true
		order[i] = index
	}
	for _, name := range declared {
		if !seen[name] {
			errs = append(errs, safeerror.New(fmt.Sprintf("missing column %q", name)))
		}
	}
	if len(errs) != 0 {
		return nil, safeerror.Join("operation failed", safeerror.NewFault(fault.Invalid, "raw result columns do not match declaration"), safeerror.Join("operation failed", errs...))
	}
	return order, nil
}
