package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type retrievalFixtureRecord struct {
	ID   int
	Name string
}

type retrievalFixtureTable struct {
	orm.Table[retrievalFixtureRecord, int]
	Name orm.Text[retrievalFixtureRecord, string]
}

func retrievalTable(opts ...orm.Option) retrievalFixtureTable {
	return orm.Define("retrieved_records", func(b orm.Builder[retrievalFixtureRecord], m *retrievalFixtureRecord) retrievalFixtureTable {
		return retrievalFixtureTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, opts...)
}

func retrievalFixtureDriver(values [][]driver.Value) *driverState {
	state := &driverState{}
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &resultRows{state: state, columns: []string{"id", "name"}, values: values}, nil
	}
	return state
}

func TestRetrievalDispatchClosesStoredRowsBeforeCallbackIO(t *testing.T) {
	for _, kind := range []string{"pool", "session", "transaction", "savepoint"} {
		t.Run(kind, func(t *testing.T) {
			state := retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}, {int64(2), "second"}})
			db := open(t, state, oneObserverConnection)
			factories := 0
			var seen []retrievalFixtureRecord
			var retained context.Context
			definition := retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
				factories++
				if state.rowsClosed.Load() != 1 {
					t.Error("factory ran before complete hydration and row closure")
				}
				return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(ctx context.Context, executor database.Executor, item retrievalFixtureRecord) error {
					retained = ctx
					if _, ok := executor.(rowObserverExecutor); !ok {
						return errors.New("retrieval callback bypassed the supplied executor wrapper")
					}
					seen = append(seen, item)
					item.Name = "local callback edit"
					_, err := executor.Exec(ctx, "SELECT 1")
					return err
				}}
			}))
			q := definition.Query()
			if _, err := orm.Compile(q); err != nil || factories != 0 {
				t.Fatal("metadata construction ran retrieval factories", err)
			}
			var items []retrievalFixtureRecord
			err := runObserverOwner(t, db, t.Context(), kind, func(executor database.Executor) error {
				var err error
				items, err = q.All(t.Context(), rowObserverExecutor{executor})
				return err
			})
			want := []retrievalFixtureRecord{{1, "stored"}, {2, "second"}}
			if err != nil || factories != 1 || !reflect.DeepEqual(items, want) || !reflect.DeepEqual(seen, want) {
				t.Fatal("stored models or once-per-fetch callback order changed", items, seen, factories, err)
			}
			if retained == nil || !errors.Is(retained.Err(), context.Canceled) || db.Stats().Owners != 0 {
				t.Fatal("retrieval context or resource ownership escaped")
			}
		})
	}
}

func TestRetrievalDispatchFailuresDiscardResults(t *testing.T) {
	for _, mode := range []string{"empty", "scan", "iterate", "close", "factory-panic", "factory-goexit", "callback-error", "callback-panic", "callback-goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("retrieval fixture failure")
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				rows := &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "stored"}}}
				switch mode {
				case "empty":
					rows.values = nil
				case "scan":
					rows.values[0][0] = "invalid integer"
				case "iterate":
					rows.nextError = cause
				case "close":
					rows.closeError = cause
				}
				return rows, nil
			}
			db := open(t, state, oneObserverConnection)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			factories, callbacks := 0, 0
			q := retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
				factories++
				switch mode {
				case "factory-panic":
					panic("private factory payload")
				case "factory-goexit":
					runtime.Goexit()
				}
				return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(context.Context, database.Executor, retrievalFixtureRecord) error {
					callbacks++
					switch mode {
					case "callback-error":
						return cause
					case "callback-panic":
						panic("private callback payload")
					case "callback-goexit":
						runtime.Goexit()
					case "cancel":
						cancel()
					}
					return nil
				}}
			})).Query()
			items, err := q.All(ctx, db)
			if mode == "empty" {
				if err != nil || items == nil || len(items) != 0 || factories != 0 || callbacks != 0 {
					t.Fatal("empty fetch invoked hooks or lost empty-slice semantics", err)
				}
			} else {
				if err == nil || items != nil {
					t.Fatal("failed retrieval published partial models", items, err)
				}
				if mode == "scan" || mode == "iterate" || mode == "close" {
					if factories != 0 || callbacks != 0 {
						t.Fatal("failed hydration constructed hooks")
					}
				}
				if mode == "factory-panic" || mode == "factory-goexit" || mode == "callback-panic" || mode == "callback-goexit" {
					if !errors.Is(err, fault.Panicked) {
						t.Fatal("panic/Goexit escaped callback isolation", err)
					}
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("callback cancellation was hidden", err)
				}
			}
			if state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("retrieval failure leaked rows or owners")
			}
		})
	}
}

type retrievalFixtureAlias struct{}

func TestRetrievalDispatchPreservesCompleteRecordSources(t *testing.T) {
	state := retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}})
	db := open(t, state, oneObserverConnection)
	callbacks := 0
	table := retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
		return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(context.Context, database.Executor, retrievalFixtureRecord) error { callbacks++; return nil }}
	}))
	q := table.Query()
	alias := rows.As[retrievalFixtureAlias](q, "r")
	cte := rows.AsRecord[retrievalFixtureAlias](rows.CTE("retrieved_cte", rows.ModelRecord(q)), "r")
	recursive := rows.RecursiveCTE("retrieved_recursive", rows.ModelRecord(q), func(self rows.RecursiveSelf[retrievalFixtureRecord]) rows.RecordSource[retrievalFixtureRecord] {
		step := rows.AsRecord[retrievalFixtureAlias](self, "step")
		return rows.SelectRecord(step, rows.Scope(step))
	})
	recursiveAlias := rows.AsRecord[retrievalFixtureAlias](recursive, "r")
	set := rows.Union(rows.ModelRecord(q), rows.ModelRecord(q))
	cursor := rows.CursorFor(rows.ModelRecord(q))
	cursorID := expr.At(cursor.Scope(), table.Key())
	cursor = cursor.UniqueBy(expr.Group(cursorID))
	for name, read := range map[string]func(context.Context, database.Executor) ([]retrievalFixtureRecord, error){
		"model":         q.All,
		"record":        rows.Read(rows.FromModel(q)).All,
		"alias":         rows.SelectRecord(alias, rows.Scope(alias)).All,
		"cte":           rows.SelectRecord(cte, rows.Scope(cte)).All,
		"recursive-cte": rows.SelectRecord(recursiveAlias, rows.Scope(recursiveAlias)).All,
		"set":           set.All,
		"set-record":    rows.SelectRecord(set, set.Scope()).All,
		"cursor": func(ctx context.Context, executor database.Executor) ([]retrievalFixtureRecord, error) {
			page, err := cursor.Paginate(ctx, executor, orm.CursorRequest[retrievalFixtureRecord]{Size: 1})
			return page.Items, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := callbacks
			items, err := read(t.Context(), rowObserverExecutor{db})
			if err != nil || len(items) != 1 || callbacks != before+1 {
				t.Fatal("complete record lost retrieval metadata", items, callbacks-before, err)
			}
		})
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		locked := q.ForUpdate()
		transactionCTE := rows.AsTransaction[retrievalFixtureAlias](rows.TransactionCTE("locked_retrieval", rows.LockedRecord(locked)), "r")
		for _, read := range []func(context.Context, *database.Tx) ([]retrievalFixtureRecord, error){
			locked.All,
			rows.SelectTransactionRecord(transactionCTE, rows.TransactionScope(transactionCTE)).All,
		} {
			before := callbacks
			items, err := read(t.Context(), tx)
			if err != nil || len(items) != 1 || callbacks != before+1 {
				return errors.Join(errors.New("locked model or transaction CTE lost retrieval metadata"), err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRetrievalDispatchUsesActualRowsRegistryAndRejectsStaleAdapters(t *testing.T) {
	called := 0
	stale, err := lifecycle.NewRetrievalObserver[retrievalFixtureRecord, struct{}]("read.fixture").Declare(func() struct{} { called++; return struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	staleSet, err := lifecycle.NewObservers(stale)
	if err != nil {
		t.Fatal(err)
	}
	staleDB := rowObserverPool(t, retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}}), staleSet)
	q := retrievalTable().Query()
	if items, err := q.All(t.Context(), rowObserverExecutor{staleDB}); !errors.Is(err, fault.Invalid) || items != nil || called != 0 {
		t.Fatal("registered read observer was silently bypassed by a stale adapter", items, called, err)
	}
	declaration, err := orm.NewRetrievalObserver[retrievalFixtureRecord]("read.fixture").Declare(func() orm.RetrievalHooks[retrievalFixtureRecord] {
		called++
		return orm.RetrievalHooks[retrievalFixtureRecord]{}
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	db := rowObserverPool(t, retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}}), set)
	if items, err := q.All(t.Context(), rowObserverExecutor{db}); err != nil || len(items) != 1 || called != 1 {
		t.Fatal("forwarding reader lost registered-only hooks", items, called, err)
	}
	other := open(t, retrievalFixtureDriver([][]driver.Value{{int64(2), "other"}}), nil)
	if items, err := q.All(t.Context(), rowObserverExecutor{other}); err != nil || len(items) != 1 || called != 1 {
		t.Fatal("observer registrations leaked between pools", items, called, err)
	}
}

func TestRetrievalDispatchSharesLifecycleDepthAndSkipsWriteHydration(t *testing.T) {
	state := retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}})
	db := open(t, state, oneObserverConnection)
	var q orm.Query[retrievalFixtureRecord]
	factories := 0
	q = retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
		factories++
		return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(ctx context.Context, executor database.Executor, _ retrievalFixtureRecord) error {
			_, err := q.All(ctx, executor)
			return err
		}}
	})).Query()
	if items, err := q.All(t.Context(), db); !errors.Is(err, fault.Invalid) || items != nil || factories != lifecore.MaxDepth || db.Stats().Owners != 0 {
		t.Fatal("recursive retrieval exceeded the shared lifecycle bound or leaked work", factories, err)
	}
	writeState := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
	writeDB := open(t, writeState, nil)
	reads, writeFactories := 0, 0
	writes := mutationDefinition(orm.WithRetrievalHooks(func() orm.RetrievalHooks[mutationRecord] {
		reads++
		return orm.RetrievalHooks[mutationRecord]{}
	}), orm.WithHooks(func() orm.Hooks[mutationRecord] { writeFactories++; return orm.Hooks[mutationRecord]{} }))
	if _, err := writes.Insert(t.Context(), writeDB, writes.Name.Set("private record name")); err != nil || reads != 0 {
		t.Fatal("write RETURNING invoked retrieval hooks", reads, err)
	}
	if _, err := writes.Update(t.Context(), writeDB, 42, writes.Name.Set("private record name")); err != nil || reads != 0 || writeFactories != 2 || writeState.rowsClosed.Load() != 3 {
		t.Fatal("write snapshots/RETURNING invoked retrieval or skipped write hooks", reads, writeFactories, err)
	}

}

func TestRetrievalDispatchUsesBoundedBatchesForStreaming(t *testing.T) {
	for _, kind := range []string{"model", "complete-record"} {
		t.Run(kind, func(t *testing.T) {
			values := make([][]driver.Value, 231)
			for i := range values {
				values[i] = []driver.Value{int64(i + 1), "stored"}
			}
			state := &driverState{}
			var sizes []int
			keyed := 0
			window := regexp.MustCompile(` LIMIT \$(\d+)(?: OFFSET \$(\d+))?$`)
			keyset := regexp.MustCompile(`"id" > \$(\d+)\)`)
			state.query = func(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
				match := window.FindStringSubmatch(sql)
				if match == nil {
					return nil, errors.New("streaming retrieval issued an unbounded SELECT")
				}
				limitIndex, _ := strconv.Atoi(match[1])
				limit := int(args[limitIndex-1].Value.(int64))
				offset := 0
				if match[2] != "" {
					offsetIndex, _ := strconv.Atoi(match[2])
					offset = int(args[offsetIndex-1].Value.(int64))
				}
				if limit > querycore.DefaultChunkSize {
					return nil, errors.New("retrieval batch exceeds its bound")
				}
				// Later batches continue after the last delivered key (ids are
				// 1-based positions), never by accumulating OFFSET.
				if key := keyset.FindStringSubmatch(sql); key != nil {
					index, _ := strconv.Atoi(key[1])
					if match[2] != "" {
						return nil, errors.New("keyset batch also used OFFSET")
					}
					offset = int(args[index-1].Value.(int64))
					keyed++
				}
				selected := values[min(offset, len(values)):min(offset+limit, len(values))]
				sizes = append(sizes, len(selected))
				return &resultRows{state: state, columns: []string{"id", "name"}, values: selected}, nil
			}
			db := open(t, state, oneObserverConnection)
			factories, retrieved := 0, 0
			q := retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
				factories++
				if state.rowsClosed.Load() != int64(len(sizes)) {
					t.Error("batch factory ran with rows open")
				}
				return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(ctx context.Context, executor database.Executor, _ retrievalFixtureRecord) error {
					retrieved++
					_, err := executor.Exec(ctx, "SELECT 1")
					return err
				}}
			})).Query().Offset(3).Limit(205)
			each := q.Each
			if kind == "complete-record" {
				each = func(ctx context.Context, ex database.Executor, yield func(retrievalFixtureRecord) error) error {
					return rows.EachRead(ctx, ex, rows.Read(rows.FromModel(q)), yield)
				}
			}
			var seen []int
			err := each(t.Context(), rowObserverExecutor{db}, func(item retrievalFixtureRecord) error {
				seen = append(seen, item.ID)
				return nil
			})
			if err != nil || !reflect.DeepEqual(sizes, []int{100, 100, 5}) || keyed != 2 || factories != 3 || retrieved != 205 || len(seen) != 205 || seen[0] != 4 || seen[len(seen)-1] != 208 {
				t.Fatal("streaming retrieval lost its batch/window contract", sizes, keyed, factories, retrieved, len(seen), err)
			}
			if db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("streaming retrieval retained resources")
			}
		})
	}
}

func TestRetrievalDispatchExcludesScalarsAndDeclaredDTOs(t *testing.T) {
	state := &driverState{}
	state.query = func(_ context.Context, sql string, _ []driver.NamedValue) (driver.Rows, error) {
		rows := &resultRows{state: state}
		switch {
		case strings.HasPrefix(sql, "SELECT COUNT("):
			rows.values = [][]driver.Value{{int64(1)}}
		case strings.HasPrefix(sql, "SELECT EXISTS("):
			rows.values = [][]driver.Value{{true}}
		default:
			rows.columns = []string{"id", "name"}
			rows.values = [][]driver.Value{{int64(1), "stored"}}
		}
		return rows, nil
	}
	db := open(t, state, nil)
	factories := 0
	table := retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
		factories++
		return orm.RetrievalHooks[retrievalFixtureRecord]{}
	}))
	q := table.Query()
	if n, err := q.Count(t.Context(), db); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if yes, err := q.Exists(t.Context(), db); err != nil || !yes {
		t.Fatal(yes, err)
	}
	// Reusing the Go type proves callback semantics follow the declaration.
	dto := rows.Define(func(b rows.Builder[retrievalFixtureRecord], m *retrievalFixtureRecord) retrievalDTO {
		return retrievalDTO{b.Row(), rows.NumberOf(b, &m.ID), rows.TextOf(b, &m.Name)}
	})
	projection := rows.Select(rows.From(q), dto, rows.Map(dto.ID, expr.Value(table.Key())), rows.Map(dto.Name, expr.Value(table.Name)))
	if items, err := projection.All(t.Context(), db); err != nil || len(items) != 1 || factories != 0 {
		t.Fatal("scalar/DTO reads acquired model retrieval callbacks", factories, err)
	}
	// Set results already use their first input's decoder/record declaration.
	// Lifecycle follows that same declaration, never the unexecuted SQL inputs.
	if items, err := rows.Union(rows.ModelRecord(q), rows.RecordOf(projection)).All(t.Context(), db); err != nil || len(items) != 1 || factories != 1 {
		t.Fatal("model-declared set lost its result lifecycle", factories, err)
	}
	if items, err := rows.Union(rows.RecordOf(projection), rows.ModelRecord(q)).All(t.Context(), db); err != nil || len(items) != 1 || factories != 1 {
		t.Fatal("DTO-declared set acquired its SQL input's model lifecycle", factories, err)
	}
}

type retrievalDTO struct {
	rows.Row[retrievalFixtureRecord]
	ID   rows.Field[retrievalFixtureRecord, int]
	Name rows.Field[retrievalFixtureRecord, string]
}
