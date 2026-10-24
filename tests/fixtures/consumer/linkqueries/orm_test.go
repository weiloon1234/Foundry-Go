package linkqueries_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type statementCapture struct {
	sql  string
	args []any
}

var errCaptured = errors.New("captured statement")

func (c *statementCapture) Query(_ context.Context, sql string, args ...any) (*database.Rows, error) {
	c.sql = sql
	c.args = append([]any(nil), args...)
	return nil, errCaptured
}
func (*statementCapture) Exec(context.Context, string, ...any) (database.Result, error) {
	return database.Result{}, errCaptured
}

func TestORMDeclarationsAndReadParity(t *testing.T) {
	if err := orm.Check(linkqueries.Members, linkqueries.Groups, linkqueries.Memberships, linkqueries.NaturalLinks, linkqueries.NullableLinks, linkqueries.Friendships, linkqueries.Archives); err != nil {
		t.Fatal(err)
	}
	old := &statementCapture{}
	_, err := linkqueries.QueryLinkMembers().Where(linkqueries.MemberFields().Name.Eq("Ada")).OrderBy(linkqueries.MemberFields().Name.Asc()).Limit(5).All(t.Context(), old)
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	statement, err := orm.Compile(linkqueries.Members.Query().Where(linkqueries.Members.Name.Eq("Ada")).OrderBy(linkqueries.Members.Name.Asc()).Limit(5))
	if err != nil {
		t.Fatal(err)
	}
	if statement.SQL() != old.sql || !reflect.DeepEqual(statement.Arguments(), old.args) {
		t.Fatalf("read SQL or bindings changed: old=%s new=%s", old.sql, statement.SQL())
	}
	for _, relation := range []orm.Relation[linkqueries.Member]{linkqueries.MemberLinks.Groups, linkqueries.MemberLinks.NamedGroups, linkqueries.MemberLinks.AliasGroups, linkqueries.MemberLinks.Friends} {
		if _, err := orm.Compile(linkqueries.Members.Query().With(relation)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresORMPivotLifecycle(t *testing.T) {
	// No generated observer family is registered in this application.
	db, clock, namespace := linkORMDatabase(t)
	var trace linkqueries.Trace
	ctx := linkqueries.WithTrace(t.Context(), &trace)
	err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		member, err := linkqueries.Members.Insert(ctx, tx, linkqueries.Members.Name.Set("member"))
		if err != nil {
			return err
		}
		group, err := linkqueries.Groups.Insert(ctx, tx, linkqueries.Groups.Key().Set("staff"), linkqueries.Groups.Name.Set("staff"))
		if err != nil {
			return err
		}
		pivot, err := orm.Attach(ctx, tx, linkqueries.MemberLinks.Groups, member, group, linkqueries.Memberships.Role.Set(" ADMIN "))
		if err != nil {
			return err
		}
		if pivot.Role != "admin" || !pivot.CreatedAt.Equal(clock.Now().Truncate(time.Microsecond)) {
			t.Fatal("pivot lost mutation or clock convention")
		}
		if trace.Reads != 0 {
			t.Fatal("write hydration dispatched retrieval hooks")
		}
		_, err = linkqueries.Members.Find(ctx, tx, member.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"local.saving", "provider.saving", "local.creating", "provider.creating", "local.created", "provider.created", "local.saved", "provider.saved"}
	if !reflect.DeepEqual(trace.Steps, want) || len(trace.ORMChanges) != 4 || len(trace.Committed) != 2 || trace.Reads != 1 {
		t.Fatalf("ORM pivot lifecycle changed: steps=%v changes=%d committed=%d reads=%d", trace.Steps, len(trace.ORMChanges), len(trace.Committed), trace.Reads)
	}
}
