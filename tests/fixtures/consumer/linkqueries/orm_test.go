package linkqueries_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

func TestORMDeclarationsAndReadParity(t *testing.T) {
	if err := orm.Check(linkqueries.Members, linkqueries.Groups, linkqueries.Memberships, linkqueries.NaturalLinks, linkqueries.NullableLinks, linkqueries.Friendships, linkqueries.Archives); err != nil {
		t.Fatal(err)
	}
	frozen, err := os.ReadFile("testdata/legacy-read.json")
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		SQL  string          `json:"sql"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(frozen, &old); err != nil {
		t.Fatal(err)
	}
	statement, err := orm.Compile(linkqueries.Members.Query().Where(orm.TextEq(linkqueries.Members.Name, "Ada")).OrderBy(orm.TextAsc(linkqueries.Members.Name)).Limit(5))
	if err != nil {
		t.Fatal(err)
	}
	bindings := statement.Arguments()
	// Preserve the old string binding and int64 LIMIT type as well as JSON values.
	if len(bindings) != 2 {
		t.Fatal("read binding count changed")
	}
	if _, ok := bindings[0].(string); !ok {
		t.Fatal("name binding type changed")
	}
	if _, ok := bindings[1].(int64); !ok {
		t.Fatal("limit binding type changed")
	}
	args, err := json.Marshal(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if statement.SQL() != old.SQL || !bytes.Equal(args, old.Args) || !oracleEqual(t, "consumer/linkqueries/orm_test/1", oracle.Snapshot([]any{statement.SQL(), json.RawMessage(args)})) {
		t.Fatalf("read SQL or bindings changed: old=%s new=%s", old.SQL, statement.SQL())
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
		member, err := orm.Insert(ctx, tx, linkqueries.Members.Table, orm.TextSet(linkqueries.Members.Name, "member"))
		if err != nil {
			return err
		}
		group, err := orm.Insert(ctx, tx, linkqueries.Groups.Table, orm.KeySet(linkqueries.Groups.Key(), "staff"), orm.TextSet(linkqueries.Groups.Name, "staff"))
		if err != nil {
			return err
		}
		pivot, err := orm.Attach(ctx, tx, linkqueries.MemberLinks.Groups, member, group, orm.InputTextSet(linkqueries.Memberships.Role, " ADMIN "))
		if err != nil {
			return err
		}
		if pivot.Role != "admin" || !pivot.CreatedAt.Equal(clock.Now().Truncate(time.Microsecond)) {
			t.Fatal("pivot lost mutation or clock convention")
		}
		if trace.Reads != 0 {
			t.Fatal("write hydration dispatched retrieval hooks")
		}
		_, err = orm.Find(ctx, tx, linkqueries.Members.Table, member.ID)
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
