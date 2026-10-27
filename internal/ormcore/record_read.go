package ormcore

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CompileRecord validates the executor kind and all retained declarations.
func CompileRecord(ctx context.Context, r RecordSource) (querycore.Statement, error) {
	err := ValidateRecordSource(r, r.Record.Type, r.Transaction)
	for _, c := range AppendChecks(r.Source.Checks, r.Record.Checks...) {
		if c != nil {
			err = safeerror.Join("operation failed", err, c())
		}
	}
	return querycore.CompileRead(r.Source.Node, ctx, r.Transaction, err, r.Record.Scan != nil)
}
func RecordExecution(ctx context.Context, ex database.Executor, transaction bool) error {
	if ctx == nil || NilDescriptor(ex) {
		return Invalid("record read requires a context and executor")
	}
	if transaction {
		if tx, ok := ex.(*database.Tx); !ok || tx == nil {
			return Invalid("record read requires a transaction")
		}
	}
	return ctx.Err()
}

// RecordCount retains input windows and nested locks in the shared SELECT compiler.
func RecordCount(ctx context.Context, ex database.Executor, r RecordSource) (int64, error) {
	if err := RecordExecution(ctx, ex, r.Transaction); err != nil {
		return 0, err
	}
	r.Source.Node = querycore.CountNode(r.Source.Node)
	s, err := CompileRecord(ctx, r)
	if err != nil {
		return 0, err
	}
	prefix, suffix := querycore.ReadEnvelope(querycore.ReadCount)
	s = s.Envelope(prefix, suffix)
	var n int64
	err = database.ScanOne(s.Context(ctx), readExecutor{ex}, s.SQLText, s.Bindings, codec.Signed[int64]().Scan(&n))
	return n, err
}
func RecordExists(ctx context.Context, ex database.Executor, r RecordSource) (bool, error) {
	if err := RecordExecution(ctx, ex, r.Transaction); err != nil {
		return false, err
	}
	r = FirstRecord(r)
	s, err := CompileRecord(ctx, r)
	if err != nil {
		return false, err
	}
	prefix, suffix := querycore.ReadEnvelope(querycore.ReadExists)
	s = s.Envelope(prefix, suffix)
	var result bool
	err = database.ScanOne(s.Context(ctx), readExecutor{ex}, s.SQLText, s.Bindings, codec.Bool[bool]().Scan(&result))
	return result, err
}
func FirstRecord(r RecordSource) RecordSource {
	if n, set := r.Source.Node.Limit.Get(); !set || n > 1 {
		r.Source.Node.Limit = value.Set(1)
	}
	return r
}

type readExecutor struct{ database.Executor }

func (e readExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	return database.ReadQuery(ctx, e.Executor, sql, args...)
}
