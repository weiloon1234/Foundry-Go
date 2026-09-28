package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Compile validates and compiles the query without executing it.
func (q ValueSetQuery[P]) Compile() (Statement, error) { return q.reader().Compile() }

// Each streams complete results and closes rows when its callback returns or fails.
func (q ValueSetQuery[P]) Each(ctx context.Context, executor database.Executor, yield func(P) error) error {
	return q.reader().Each(ctx, executor, yield)
}

// All collects results, discarding partial output on failure.
func (q ValueSetQuery[P]) All(ctx context.Context, executor database.Executor) ([]P, error) {
	return q.reader().All(ctx, executor)
}

// First returns an optional result from the selected window. Order explicitly for deterministic selection.
func (q ValueSetQuery[P]) First(ctx context.Context, executor database.Executor) (value.Optional[P], error) {
	return q.reader().First(ctx, executor)
}

// RequireFirst returns database.NotFound when the selected window is empty.
func (q ValueSetQuery[P]) RequireFirst(ctx context.Context, executor database.Executor) (P, error) {
	return q.reader().RequireFirst(ctx, executor)
}

// Count counts the selected result window, including grouping and pagination.
func (q ValueSetQuery[P]) Count(ctx context.Context, executor database.Executor) (int64, error) {
	return q.reader().Count(ctx, executor)
}

// Exists reports whether the selected result window contains a row.
func (q ValueSetQuery[P]) Exists(ctx context.Context, executor database.Executor) (bool, error) {
	return q.reader().Exists(ctx, executor)
}
