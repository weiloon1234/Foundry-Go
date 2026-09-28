package retrievalqueries_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/retrievalqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type factoryStats struct {
	Member [2]atomic.Int64
	Group  atomic.Int64
	Pivot  atomic.Int64
}

func memberConstructor(key foundation.Key[*factoryStats], index int, label string) func(foundation.Resolver) (func() retrievalqueries.MemberRetrievalHooks, error) {
	return func(resolver foundation.Resolver) (func() retrievalqueries.MemberRetrievalHooks, error) {
		stats, err := foundation.Resolve(resolver, key)
		if err != nil {
			return nil, err
		}
		return func() retrievalqueries.MemberRetrievalHooks {
			stats.Member[index].Add(1)
			return retrievalqueries.MemberRetrievalHooks{Retrieved: func(ctx context.Context, executor database.Executor, member retrievalqueries.Member) error {
				return retrievalqueries.Observe(ctx, executor, label, member.Name)
			}}
		}, nil
	}
}

func retrievalApp(t *testing.T) (*database.DB, *factoryStats, string) {
	t.Helper()
	pool := foundation.NewKey[*database.DB]("retrieval.pool")
	statsKey := foundation.NewKey[*factoryStats]("retrieval.stats")
	stats := &factoryStats{}
	config := pgtest.Config(t)
	config.Pool.MaxOpen, config.Pool.MaxIdle = 1, 1
	config.Pool.AcquireTimeout = 2 * time.Second
	app := testkit.Start(t, foundry.New().Register(
		foundation.Module{Name: "second", Requires: []foundation.ProviderID{"first"}, OnRegister: func(r *foundation.Registrar) error {
			return retrievalqueries.RegisterMemberRetrievalObserver(r, pool, retrievalqueries.NewMemberRetrievalObserver("members.second"), memberConstructor(statsKey, 1, "second"))
		}},
		postgres.Module("database", pool, config),
		foundation.Module{Name: "first", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, statsKey, stats); err != nil {
				return err
			}
			if err := retrievalqueries.RegisterMemberRetrievalObserver(r, pool, retrievalqueries.NewMemberRetrievalObserver("members.first"), memberConstructor(statsKey, 0, "first")); err != nil {
				return err
			}
			if err := retrievalqueries.RegisterGroupRetrievalObserver(r, pool, retrievalqueries.NewGroupRetrievalObserver("groups.read"), func(s foundation.Resolver) (func() retrievalqueries.GroupRetrievalHooks, error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() retrievalqueries.GroupRetrievalHooks {
					stats.Group.Add(1)
					return retrievalqueries.GroupRetrievalHooks{Retrieved: func(ctx context.Context, executor database.Executor, group retrievalqueries.Group) error {
						return retrievalqueries.Observe(ctx, executor, "group", group.Name)
					}}
				}, nil
			}); err != nil {
				return err
			}
			return retrievalqueries.RegisterMembershipRetrievalObserver(r, pool, retrievalqueries.NewMembershipRetrievalObserver("memberships.read"), func(s foundation.Resolver) (func() retrievalqueries.MembershipRetrievalHooks, error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() retrievalqueries.MembershipRetrievalHooks {
					stats.Pivot.Add(1)
					return retrievalqueries.MembershipRetrievalHooks{Retrieved: func(ctx context.Context, executor database.Executor, membership retrievalqueries.Membership) error {
						code, _ := membership.GroupCode.Get()
						return retrievalqueries.Observe(ctx, executor, "pivot", string(code))
					}}
				}, nil
			})
		}},
	))
	db, err := foundation.Resolve(app.Services(), pool)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	if err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
		for _, ddl := range []string{
			`CREATE TABLE retrieval_members(id uuid PRIMARY KEY,name text NOT NULL,nickname text,parent_id uuid)`,
			`CREATE TABLE retrieval_groups(code text PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE retrieval_memberships(id uuid PRIMARY KEY,member_id uuid NOT NULL,group_code text)`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		root, err := retrievalqueries.QueryRetrievalMembers().Create(t.Context(), tx, retrievalqueries.MemberDraft{}.SetName(" ROOT ").SetNickname("boss"))
		if err != nil {
			return err
		}
		if _, err := retrievalqueries.QueryRetrievalMembers().Create(t.Context(), tx, retrievalqueries.MemberDraft{}.SetName(" Child ").SetParentID(root.ID)); err != nil {
			return err
		}
		if _, err := retrievalqueries.QueryRetrievalGroups().Create(t.Context(), tx, retrievalqueries.GroupDraft{}.SetCode("team").SetName("stored group")); err != nil {
			return err
		}
		_, err = retrievalqueries.QueryRetrievalMemberships().Create(t.Context(), tx, retrievalqueries.MembershipDraft{}.SetMemberID(root.ID).SetGroupCode("team"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stats.Member[0].Load() != 0 || stats.Member[1].Load() != 0 || stats.Group.Load() != 0 || stats.Pivot.Load() != 0 {
		t.Fatal("seed writes constructed retrieval factories")
	}
	return db, stats, namespace
}

func inSchema(ctx context.Context, db database.Transactor, namespace string, fn func(*database.Tx) error) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		return fn(tx)
	})
}

type readWrapper struct {
	database.Executor
	queries int
}

func (r *readWrapper) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	r.queries++
	return r.Executor.Query(ctx, sql, args...)
}

func TestPostgresRetrievalOrderStoredValuesAndWrapperIO(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	var trace retrievalqueries.Trace
	ctx := retrievalqueries.WithTrace(t.Context(), &trace)
	err := inSchema(ctx, db, namespace, func(tx *database.Tx) error {
		reader := &readWrapper{Executor: tx}
		items, err := retrievalqueries.QueryRetrievalMembers().OrderBy(retrievalqueries.MemberFields().Name.Asc()).All(ctx, reader)
		if err != nil {
			return err
		}
		if len(items) != 2 || items[0].Name != "child" || items[1].Name != "root" || reader.queries != 7 {
			return errors.New("stored values or callback executor changed")
		}
		if display, err := items[0].AccessName(); err != nil || display != "CHILD" || items[0].Name != "child" {
			return errors.New("getter changed stored hydration")
		}
		if nickname, ok := items[1].Nickname.Get(); !ok || nickname != "boss" || !items[0].Nickname.IsNull() {
			return errors.New("retrieval changed nullable storage")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trace.Calls, []string{"local", "first", "second", "local", "first", "second"}) || trace.LocalFactories != 1 || stats.Member[0].Load() != 1 || stats.Member[1].Load() != 1 {
		t.Fatal("retrieval factory/stage order", trace.Calls, trace.LocalFactories)
	}
	for _, ctx := range trace.Contexts {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("retrieval context escaped")
		}
	}
}

func TestPostgresRetrievalDirectThroughAndExplicitLoad(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	var trace retrievalqueries.Trace
	ctx := retrievalqueries.WithTrace(t.Context(), &trace)
	err := inSchema(ctx, db, namespace, func(tx *database.Tx) error {
		q := retrievalqueries.QueryRetrievalMembers().Where(retrievalqueries.MemberFields().Name.Eq("root"))
		relations := retrievalqueries.MemberRelations()
		items, err := q.With(relations.Children, relations.Groups).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 {
			return errors.New("missing parent")
		}
		children, childrenLoaded := items[0].Children.Get()
		links, linksLoaded := items[0].Groups.Get()
		if !childrenLoaded || len(children) != 1 || children[0].Name != "child" || !linksLoaded || len(links) != 1 || links[0].Model.Code != "team" {
			return errors.New("typed relation hydration changed")
		}
		if code, ok := links[0].Pivot.GroupCode.Get(); !ok || code != "team" {
			return errors.New("nullable natural relation key changed")
		}
		before := len(trace.Calls)
		if _, err := q.With(relations.Children, relations.Groups).LoadMissing(ctx, tx, items); err != nil {
			return err
		}
		if len(trace.Calls) != before {
			return errors.New("LoadMissing refired loaded models")
		}
		trace.Calls = nil
		if _, err := q.With(relations.Groups).Load(ctx, tx, items); err != nil {
			return err
		}
		if !reflect.DeepEqual(trace.Calls, []string{"group", "pivot"}) {
			return errors.New("explicit Load refired its parent or skipped through models")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Member[0].Load() != 2 || stats.Member[1].Load() != 2 || trace.LocalFactories != 2 || stats.Group.Load() != 2 || stats.Pivot.Load() != 2 {
		t.Fatal("relation factories lost their model/batch ownership")
	}
}

func TestPostgresRetrievalFailureRollsBackOwningWrite(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db, _, namespace := retrievalApp(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			trace := retrievalqueries.Trace{FailAt: "first", Mode: mode, Cancel: cancel}
			ctx = retrievalqueries.WithTrace(ctx, &trace)
			err := inSchema(ctx, db, namespace, func(tx *database.Tx) error {
				if _, err := retrievalqueries.QueryRetrievalMembers().Create(ctx, tx, retrievalqueries.MemberDraft{}.SetName("rollback candidate")); err != nil {
					return err
				}
				items, err := retrievalqueries.QueryRetrievalMembers().Where(retrievalqueries.MemberFields().Name.Eq("rollback candidate")).All(ctx, tx)
				if items != nil {
					return errors.New("failed retrieval published a partial collection")
				}
				return err
			})
			want := retrievalqueries.Veto
			if mode == "panic" || mode == "goexit" {
				want = fault.Panicked
			}
			if mode == "cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || !reflect.DeepEqual(trace.Calls, []string{"local", "first"}) {
				t.Fatal("callback failure or stop order changed", trace.Calls, err)
			}
			if err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
				n, err := retrievalqueries.QueryRetrievalMembers().Where(retrievalqueries.MemberFields().Name.Eq("rollback candidate")).Count(t.Context(), tx)
				if err == nil && n != 0 {
					return errors.New("read failure committed its owning write")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
