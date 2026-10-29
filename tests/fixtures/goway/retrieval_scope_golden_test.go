package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/reads"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"reflect"
	"strings"
	"testing"
)

func readReply(sql string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
	if strings.Contains(sql, "COUNT") {
		return []string{"count"}, [][]driver.Value{{int64(1)}}
	}
	if strings.HasPrefix(sql, `SELECT "foundry_target".`) {
		return []string{"id", "tenant_id", "name", "id", "tenant_id", "member_id", "group_id"}, [][]driver.Value{{int64(3), int64(7), "group", int64(4), int64(7), int64(1), int64(3)}}
	}
	if strings.Contains(sql, "ANY(") {
		return []string{"id", "tenant_id", "name", "manager_id"}, [][]driver.Value{{int64(2), int64(7), "manager", nil}}
	}
	return []string{"id", "tenant_id", "name", "manager_id"}, [][]driver.Value{{int64(1), int64(7), "member", int64(2)}}
}
func TestRetrievalAndScopeOldAndNewAgree(t *testing.T) {
	var transcripts []string
	{
		capture := &goldenCapture{reply: readReply}
		var trace []string
		ctx := coremodels.TenantContext(coremodels.LifecycleContext(t.Context(), &trace), 7)
		db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
		if err != nil {
			t.Fatal(err)
		}
		var declarations []lifecycle.Declaration

		d, err := orm.NewRetrievalObserver[n.Member]("member.read").Declare(func() orm.RetrievalHooks[n.Member] { return n.ReadHooks("observer") })
		check(err)
		declarations = append(declarations, d)
		d, err = orm.NewRetrievalObserver[n.Group]("group.read").Declare(func() orm.RetrievalHooks[n.Group] {
			return orm.RetrievalHooks[n.Group]{Retrieved: func(ctx context.Context, _ database.Executor, _ n.Group) error {
				coremodels.TraceLifecycle(ctx, "group.Retrieved")
				return nil
			}}
		})
		check(err)
		declarations = append(declarations, d)
		d, err = orm.NewRetrievalObserver[n.Membership]("pivot.read").Declare(func() orm.RetrievalHooks[n.Membership] {
			return orm.RetrievalHooks[n.Membership]{Retrieved: func(ctx context.Context, _ database.Executor, _ n.Membership) error {
				coremodels.TraceLifecycle(ctx, "pivot.Retrieved")
				return nil
			}}
		})
		check(err)
		declarations = append(declarations, d)

		set, err := lifecycle.NewObservers(declarations...)
		check(err)
		check(db.BindObservers(set))
		check(db.Start(ctx))
		t.Cleanup(func() { check(db.Close(context.Background())) })

		q := n.Members.Query()
		items, err := orm.All(ctx, db, q.Where(orm.TextEq(n.Members.Name, "explicit")))
		check(err)
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{items[0].ID, items[0].Name}))
		_, err = orm.Count(ctx, db, q)
		check(err)
		_, err = orm.First(ctx, db, orm.WithoutGlobalScope(q, n.MemberTenant(n.Members)).Where(orm.KeyEq(n.Members.ID, 1)))
		check(err)
		_, err = orm.All(ctx, db, q.With(n.Manager))
		check(err)
		_, err = orm.All(ctx, db, q.With(n.MemberGroups))
		check(err)
		_, err = orm.All(ctx, db, q.Where(orm.Has(n.MemberGroups)))
		check(err)
		_, err = orm.Insert(ctx, db, n.Members.Table, orm.KeySet(n.Members.ID, 1), orm.NumberSet(n.Members.TenantID, 7), orm.TextSet(n.Members.Name, "member"))
		check(err)

		fmt.Fprintln(&capture.output, oracleSnapshot([]any{trace}))
		if !reflect.DeepEqual(trace, []string{"local.Retrieved", "observer.Retrieved", "local.Retrieved", "observer.Retrieved", "local.Retrieved", "observer.Retrieved", "local.Retrieved", "observer.Retrieved", "local.Retrieved", "observer.Retrieved", "group.Retrieved", "pivot.Retrieved", "local.Retrieved", "observer.Retrieved"}) {
			t.Fatal("retrieval dispatch coverage", trace)
		}
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "retrieval_scope_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}

type readSide struct{}
type groupSide struct{}
type readName struct{ Name string }
type readNameFields struct {
	rowset.Row[readName]
	Name rowset.Field[readName, string]
}
type readCount struct{ Count int64 }
type readCountFields struct {
	rowset.Row[readCount]
	Count rowset.Field[readCount, int64]
}

var readCounts = rowset.Define(func(b rowset.Builder[readCount], r *readCount) readCountFields {
	return readCountFields{b.Row(), rowset.NumberOf(b, &r.Count)}
})
var readNames = rowset.Define(func(b rowset.Builder[readName], r *readName) readNameFields {
	return readNameFields{b.Row(), rowset.TextOf(b, &r.Name)}
})
