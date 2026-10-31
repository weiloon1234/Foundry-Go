package rowsterminaltest

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type readAlias struct{}

func TestReadTerminalsScalarAndDTOStreams(t *testing.T) {
	table := orm.Define("members", func(b orm.Builder[streamRecord], m *streamRecord) streamTable {
		return streamTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	})
	scalar := rows.SelectScalar(rows.From(table.Query()), expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(table.Name)))))
	values := rows.Values(scalar)
	cte := rows.AsRecord[readAlias](rows.CTE("names", rows.ModelRecord(table.Query())), "n")
	for _, kind := range []string{"scalar", "values", "cte"} {
		for _, mode := range []string{"stop", "cancel", "panic", "goexit", "scan", "close", "canceled", "nil"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				var q rows.ReadSource[streamRecord, string] = rows.ReadScalar(scalar)
				state := &streamState{columns: []string{"value"}, values: [][]driver.Value{{"Ada"}, {"Next"}}}
				stop := errors.New("stop scalar stream")
				if mode == "scan" {
					state.values[0][0] = struct{}{}
				}
				if mode == "close" {
					state.closeErr = stop
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if mode == "canceled" {
					cancel()
				}
				seen := 0
				yield := func(string) error {
					seen++
					switch mode {
					case "stop":
						return stop
					case "cancel":
						cancel()
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					}
					return nil
				}
				if mode == "nil" {
					yield = nil
				}
				db := openStreamDB(t, state)
				var err error
				switch kind {
				case "scalar":
					err = rows.EachRead(ctx, db, q, yield)
				case "values":
					err = rows.EachRead(ctx, db, rows.ReadValues(rows.UnionAllValues(values, values)), yield)
				case "cte":
					err = rows.EachRead(ctx, db, rows.ReadScalar(rows.SelectScalar(cte, expr.AsSelected(expr.Row(expr.At(rows.Scope(cte), orm.TextExpr(table.Name)))))), yield)
				}
				switch mode {
				case "stop", "close":
					if !errors.Is(err, stop) {
						t.Fatal(err)
					}
				case "cancel", "canceled":
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				case "panic", "goexit":
					if !errors.Is(err, fault.Panicked) {
						t.Fatal(err)
					}
				case "nil":
					if !errors.Is(err, fault.Invalid) {
						t.Fatal(err)
					}
				case "scan":
					if err == nil {
						t.Fatal("invalid scalar decoded")
					}
				}
				if mode == "nil" || mode == "canceled" {
					if state.queries != 0 {
						t.Fatal("invalid call executed")
					}
				} else if !state.closed.Load() || state.queries != 1 {
					t.Fatal("rows leaked")
				}
				if mode == "stop" && seen != 1 {
					t.Fatal("early stop delivered extra rows")
				}
				if db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("owner leaked")
				}
			})
		}
	}
	// Declared DTOs retain row-at-a-time delivery, even if the Go type also has a model.
	q := rows.Read(streamProjection())
	state := &streamState{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "Ada"}, {"invalid", "Next"}}}
	seen := 0
	if err := rows.EachRead(t.Context(), openStreamDB(t, state), q, func(streamRecord) error { seen++; return nil }); err == nil || seen != 1 || !state.closed.Load() {
		t.Fatal(seen, err)
	}
}

func TestReadTerminalValidationAndContext(t *testing.T) {
	var absent rows.ReadSource[streamRecord, streamRecord]
	for _, q := range []rows.ReadSource[streamRecord, streamRecord]{absent, rows.RecordRead[streamRecord, streamRecord]{}, rows.Result[streamRecord, streamRecord]{}} {
		if _, err := rows.CompileRead(q); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if err := rows.PreflightRead(t.Context(), q); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		state := &streamState{}
		if err := rows.EachRead(t.Context(), openStreamDB(t, state), q, func(streamRecord) error { return nil }); !errors.Is(err, fault.Invalid) || state.queries != 0 {
			t.Fatal(err)
		}
	}
	q := rows.Read(streamProjection())
	if err := rows.PreflightRead(nil, q); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := rows.EachRead(nil, nil, q, func(streamRecord) error { return nil }); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := rows.EachRead(t.Context(), (*database.DB)(nil), q, func(streamRecord) error { return nil }); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rows.PreflightRead(ctx, q); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReadTerminalsDoNotEraseTransactionsOrZeroWindowValidation(t *testing.T) {
	table := orm.Define("members", func(b orm.Builder[streamRecord], m *streamRecord) streamTable {
		return streamTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[streamRecord] {
		t.Error("zero window constructed factory")
		return orm.RetrievalHooks[streamRecord]{}
	}))
	locked := rows.AsTransaction[readAlias](rows.LockedRecord(orm.Lock(table.Query())), "r")
	transaction := rows.SelectTransactionRecord(locked, rows.TransactionScope(locked))
	if _, err := rows.CompileRead(transaction); !errors.Is(err, fault.Invalid) {
		t.Fatal("transaction escaped", err)
	}
	state := &streamState{}
	if err := rows.EachRead(t.Context(), openStreamDB(t, state), transaction, func(streamRecord) error { return nil }); !errors.Is(err, fault.Invalid) || state.queries != 0 {
		t.Fatal(err)
	}
	a := rows.AsRecord[readAlias](rows.ModelRecord(table.Query()), "r")
	q := rows.SelectRecord(a, rows.Scope(a)).Limit(0)
	if err := rows.EachRead(t.Context(), openStreamDB(t, state), q, func(streamRecord) error { t.Error("zero window delivered"); return nil }); err != nil || state.queries != 0 {
		t.Fatal(err)
	}
	q = q.Where(expr.Predicate[rows.Alias[readAlias, streamRecord]]{})
	if err := rows.EachRead(t.Context(), openStreamDB(t, state), q, func(streamRecord) error { return nil }); !errors.Is(err, fault.Invalid) || state.queries != 0 {
		t.Fatal("zero window skipped validation", err)
	}
}

func TestReadPreflightAndIterationUseExecutingContext(t *testing.T) {
	type tenantKey struct{}
	table := orm.Define("members", func(b orm.Builder[streamRecord], m *streamRecord) streamTable {
		return streamTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, orm.WithGlobalScopes(func(table streamTable) []orm.GlobalScope[streamRecord] {
		return []orm.GlobalScope[streamRecord]{orm.Scope(table.Table, "tenant", func(ctx context.Context) (orm.Predicate[streamRecord], error) {
			id, ok := ctx.Value(tenantKey{}).(int64)
			if !ok {
				return orm.Predicate[streamRecord]{}, errors.New("tenant required")
			}
			return orm.KeyEq(table.Key(), id), nil
		})}
	}))
	bound := context.WithValue(t.Context(), tenantKey{}, int64(1))
	executing := context.WithValue(t.Context(), tenantKey{}, int64(2))
	q := rows.ReadScalar(rows.SelectScalar(rows.From(orm.WithScopeContext(table.Query(), bound)), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(table.Key()))))))
	if err := rows.PreflightRead(t.Context(), q); err == nil {
		t.Fatal("preflight used bound context")
	}
	if err := rows.PreflightRead(executing, q); err != nil {
		t.Fatal(err)
	}
	state := &streamState{columns: []string{"value"}, values: [][]driver.Value{{int64(2)}}}
	if err := rows.EachRead(executing, openStreamDB(t, state), q, func(id int64) error {
		if id != 2 {
			t.Fatal(id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(state.args) != 1 || state.args[0].Value != int64(2) {
		t.Fatal("iteration used bound context", state.args)
	}
}
