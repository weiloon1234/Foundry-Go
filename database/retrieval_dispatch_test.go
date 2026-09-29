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
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type retrievalFixtureRecord struct {
	ID   int
	Name string
}

func retrievalFixtureDefinition() query.Definition[retrievalFixtureRecord] {
	return query.Define("retrieved_records", "id", []query.Column{{Name: "id"}, {Name: "name"}},
		func(row database.Row) (retrievalFixtureRecord, error) {
			var item retrievalFixtureRecord
			err := row.Scan(&item.ID, &item.Name)
			return item, err
		},
		query.NewModelField("id", codec.Signed[int](), func(item retrievalFixtureRecord) int { return item.ID }),
		query.NewModelField("name", codec.String[string](), func(item retrievalFixtureRecord) string { return item.Name }))
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
			definition := retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
				factories++
				if state.rowsClosed.Load() != 1 {
					return query.RetrievalHooks[retrievalFixtureRecord]{}, errors.New("factory ran before complete hydration and row closure")
				}
				return query.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(ctx context.Context, executor database.Executor, item retrievalFixtureRecord) error {
					retained = ctx
					if _, ok := executor.(rowObserverExecutor); !ok {
						return errors.New("retrieval callback bypassed the supplied executor wrapper")
					}
					seen = append(seen, item)
					item.Name = "local callback edit"
					_, err := executor.Exec(ctx, "SELECT 1")
					return err
				}}, nil
			}, true)
			q := query.ForModel(definition)
			if _, err := q.Compile(); err != nil || factories != 0 {
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
	for _, mode := range []string{"empty", "scan", "iterate", "close", "factory-error", "factory-panic", "factory-goexit", "callback-error", "callback-panic", "callback-goexit", "cancel"} {
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
			q := query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
				factories++
				switch mode {
				case "factory-error":
					return query.RetrievalHooks[retrievalFixtureRecord]{}, cause
				case "factory-panic":
					panic("private factory payload")
				case "factory-goexit":
					runtime.Goexit()
				}
				return query.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(context.Context, database.Executor, retrievalFixtureRecord) error {
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
				}}, nil
			}, true))
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
	q := query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
		return query.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(context.Context, database.Executor, retrievalFixtureRecord) error { callbacks++; return nil }}, nil
	}, true))
	alias := query.As[retrievalFixtureAlias](q, "r")
	cte := query.As[retrievalFixtureAlias](query.CTE("retrieved_cte", q), "r")
	recursive := query.RecursiveCTE("retrieved_recursive", q, func(self query.RecursiveSelf[retrievalFixtureRecord]) query.RecordQuerySource[retrievalFixtureRecord] {
		step := query.As[retrievalFixtureAlias](self, "step")
		return query.SelectRecord(step, step.Scope())
	})
	recursiveAlias := query.As[retrievalFixtureAlias](recursive, "r")
	set := q.Union(q)
	cursor := query.CursorFor(q)
	cursorID := query.NewScalarField[query.CursorScope[retrievalFixtureRecord]](cursor.Scope().Table(), "id", codec.Signed[int]())
	cursor = cursor.UniqueBy(cursorID.Group())
	for name, read := range map[string]func(context.Context, database.Executor) ([]retrievalFixtureRecord, error){
		"model":         q.All,
		"record":        query.SelectRecord(q, q.Scope()).All,
		"alias":         query.SelectRecord(alias, alias.Scope()).All,
		"cte":           query.SelectRecord(cte, cte.Scope()).All,
		"recursive-cte": query.SelectRecord(recursiveAlias, recursiveAlias.Scope()).All,
		"set":           set.All,
		"set-record":    query.SelectRecord(set, set.Scope()).All,
		"cursor": func(ctx context.Context, executor database.Executor) ([]retrievalFixtureRecord, error) {
			page, err := cursor.Paginate(ctx, executor, query.CursorRequest[retrievalFixtureRecord]{Size: 1})
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
		transactionCTE := query.AsTransaction[retrievalFixtureAlias](query.TransactionCTE("locked_retrieval", locked), "r")
		for _, read := range []func(context.Context, *database.Tx) ([]retrievalFixtureRecord, error){
			locked.All,
			query.SelectTransactionRecord(transactionCTE, transactionCTE.Scope()).All,
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
	declaration, err := lifecycle.NewRetrievalObserver[retrievalFixtureRecord, query.RetrievalHooks[retrievalFixtureRecord]]("read.fixture").Declare(func() query.RetrievalHooks[retrievalFixtureRecord] {
		called++
		return query.RetrievalHooks[retrievalFixtureRecord]{}
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	db := rowObserverPool(t, retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}}), set)
	reader := rowObserverExecutor{db}
	if items, err := query.ForModel(retrievalFixtureDefinition()).All(t.Context(), reader); !errors.Is(err, fault.Invalid) || items != nil || called != 0 {
		t.Fatal("registered read observer was silently bypassed by a stale adapter", items, called, err)
	}
	q := query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(_ context.Context, set lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
		factories, err := lifecycle.RetrievalObserverFactories[retrievalFixtureRecord, query.RetrievalHooks[retrievalFixtureRecord]](set)
		if err != nil {
			return query.RetrievalHooks[retrievalFixtureRecord]{}, err
		}
		if len(factories) != 1 {
			return query.RetrievalHooks[retrievalFixtureRecord]{}, errors.New("wrong pool observer ownership")
		}
		return factories[0](), nil
	}, false))
	if items, err := q.All(t.Context(), reader); err != nil || len(items) != 1 || called != 1 {
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
	var q query.Query[retrievalFixtureRecord]
	factories := 0
	q = query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
		factories++
		return query.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(ctx context.Context, executor database.Executor, _ retrievalFixtureRecord) error {
			_, err := q.All(ctx, executor)
			return err
		}}, nil
	}, true))
	if items, err := q.All(t.Context(), db); !errors.Is(err, fault.Invalid) || items != nil || factories != query.MaxLifecycleDepth || db.Stats().Owners != 0 {
		t.Fatal("recursive retrieval exceeded the shared lifecycle bound or leaked work", factories, err)
	}
	writeState := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
	writeDB := open(t, writeState, nil)
	reads, writeFactories := 0, 0
	writes := query.ForModel(mutationDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[mutationRecord], error) {
		reads++
		return query.RetrievalHooks[mutationRecord]{}, nil
	}, true).WithWriteHooks(func() query.WriteHooks[mutationRecord] {
		writeFactories++
		return query.WriteHooks[mutationRecord]{}
	}))
	if _, err := writes.Insert(t.Context(), writeDB, mutationValues()); err != nil || reads != 0 {
		t.Fatal("write RETURNING invoked retrieval hooks", reads, err)
	}
	id := query.NewScalarField[mutationRecord]("records", "id", codec.Signed[int64]())
	if _, err := writes.Where(id.Eq(42)).Patch(t.Context(), writeDB, mutationValues()); err != nil || reads != 0 || writeFactories != 2 || writeState.rowsClosed.Load() != 3 {
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
				if limit > query.DefaultChunkSize {
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
			q := query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
				factories++
				if state.rowsClosed.Load() != int64(len(sizes)) {
					return query.RetrievalHooks[retrievalFixtureRecord]{}, errors.New("batch factory ran with rows open")
				}
				return query.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(ctx context.Context, executor database.Executor, _ retrievalFixtureRecord) error {
					retrieved++
					_, err := executor.Exec(ctx, "SELECT 1")
					return err
				}}, nil
			}, true)).Offset(3).Limit(205)
			each := q.Each
			if kind == "complete-record" {
				each = query.SelectRecord(q, q.Scope()).Each
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
	q := query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
		factories++
		return query.RetrievalHooks[retrievalFixtureRecord]{}, nil
	}, true))
	if n, err := q.Count(t.Context(), db); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if yes, err := q.Exists(t.Context(), db); err != nil || !yes {
		t.Fatal(yes, err)
	}
	id := query.NewScalarField[retrievalFixtureRecord]("retrieved_records", "id", codec.Signed[int]())
	name := query.NewTextField[retrievalFixtureRecord]("retrieved_records", "name", codec.String[string]())
	// Deliberately reuse the Go type at this explicit declaration boundary to
	// prove that callback semantics follow metadata, not a reflected result type.
	pid, pname := query.NewProjectionField[retrievalFixtureRecord, int]("id"), query.NewProjectionField[retrievalFixtureRecord, string]("name")
	definition := query.DefineProjection([]query.ProjectionColumn[retrievalFixtureRecord]{pid.Column(), pname.Column()}, func(row database.Row) (retrievalFixtureRecord, error) {
		var item retrievalFixtureRecord
		err := row.Scan(&item.ID, &item.Name)
		return item, err
	})
	projection := query.Project(q, definition, query.Map(pid, id.Value()), query.Map(pname, name.Value()))
	if items, err := projection.All(t.Context(), db); err != nil || len(items) != 1 || factories != 0 {
		t.Fatal("scalar/DTO reads acquired model retrieval callbacks", factories, err)
	}
	// Set results already use their first input's decoder/record declaration.
	// Lifecycle follows that same declaration, never the unexecuted SQL inputs.
	if items, err := q.Union(projection).All(t.Context(), db); err != nil || len(items) != 1 || factories != 1 {
		t.Fatal("model-declared set lost its result lifecycle", factories, err)
	}
	if items, err := projection.Union(q).All(t.Context(), db); err != nil || len(items) != 1 || factories != 1 {
		t.Fatal("DTO-declared set acquired its SQL input's model lifecycle", factories, err)
	}
}
