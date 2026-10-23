package ormcore

import (
	"context"
	"errors"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Raw validates the complete result shape before iteration, then reuses the
// caller's decoder. It deliberately neither routes reads nor applies scopes.
func Raw(ctx context.Context, ex database.Executor, columns []string, statement string, args []any, scan func(database.Row) error) error {
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
		return errors.Join(err, rows.Close())
	}
	order, err := rawColumnOrder(columns, actual)
	if err != nil {
		return errors.Join(err, rows.Close())
	}
	mapped := &rawRow{Rows: rows, order: order, destinations: make([]any, len(order))}
	return Consume(ctx, mapped, scan)
}

// rawRow preserves the stream's FieldEncryption scope while ordering scanner
// destinations by the actual result columns instead of declaration order.
type rawRow struct {
	*database.Rows
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
	return r.Rows.Scan(r.destinations...)
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
			errs = append(errs, fmt.Errorf("unexpected column %q", name))
		}
		if seen[name] {
			errs = append(errs, fmt.Errorf("duplicate column %q", name))
		}
		seen[name] = true
		order[i] = index
	}
	for _, name := range declared {
		if !seen[name] {
			errs = append(errs, fmt.Errorf("missing column %q", name))
		}
	}
	if len(errs) != 0 {
		return nil, errors.Join(fault.New(fault.Invalid, "raw result columns do not match declaration"), errors.Join(errs...))
	}
	return order, nil
}
