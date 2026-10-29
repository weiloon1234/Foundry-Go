package pgsmoke_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type sourceEntry struct {
	ID, Target, Tenant int64
	Name               string
}
type sourceEntries struct {
	orm.Table[sourceEntry, int64]
	Target, Tenant orm.Number[sourceEntry, int64]
	Name           orm.Text[sourceEntry, string]
}
type sourceArchive struct {
	ID, Tenant           int64
	Name, Tag            string
	Visits               int64 `foundry:"default=database"`
	CreatedAt, UpdatedAt time.Time
	DeletedAt            value.Nullable[time.Time]
}
type sourceArchives struct {
	orm.Table[sourceArchive, int64]
	ID             orm.Primary[sourceArchive, int64]
	Soft           orm.SoftDeletable[sourceArchive]
	Tenant, Visits orm.Number[sourceArchive, int64]
	Name           orm.Text[sourceArchive, string]
	Tag            orm.InputText[sourceArchive, string, string]
	CreatedAt      orm.CreatedAt[sourceArchive, time.Time]
	UpdatedAt      orm.UpdatedAt[sourceArchive, time.Time]
	DeletedAt      orm.SoftDelete[sourceArchive, time.Time]
}
type writeTenant struct{}

// The lead executes this retained PostgreSQL scenario; Lane B compiles and vets it.
func TestSourceWritesPersistScopesConventionsAndRollback(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := context.WithValue(t.Context(), writeTenant{}, int64(1))
	_, err := db.Exec(ctx, `CREATE TABLE source_entries(id bigint PRIMARY KEY,target bigint NOT NULL,tenant bigint NOT NULL,name text NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE TABLE source_archives(id bigint PRIMARY KEY,tenant bigint NOT NULL,name text NOT NULL,tag text NOT NULL,visits bigint NOT NULL DEFAULT 7,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	s := orm.Define("source_entries", func(b orm.Builder[sourceEntry], m *sourceEntry) sourceEntries {
		return sourceEntries{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Target), orm.NumberOf(b, &m.Tenant), orm.TextOf(b, &m.Name)}
	}, orm.WithGlobalScopes(func(f sourceEntries) []orm.GlobalScope[sourceEntry] {
		return []orm.GlobalScope[sourceEntry]{orm.Scope(f.Table, "tenant", func(ctx context.Context) (orm.Predicate[sourceEntry], error) {
			v, ok := ctx.Value(writeTenant{}).(int64)
			if !ok {
				return orm.Predicate[sourceEntry]{}, errors.New("missing tenant")
			}
			return orm.NumberEq(f.Tenant, v), nil
		})}
	}))
	mutations, callbacks := 0, 0
	d := orm.Define("source_archives", func(b orm.Builder[sourceArchive], m *sourceArchive) sourceArchives {
		table := orm.TableOf(b, &m.ID)
		deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
		return sourceArchives{table, orm.PrimaryOf(b, table.Key()), orm.SoftDeletes(deleted), orm.NumberOf(b, &m.Tenant), orm.NumberOf(b, &m.Visits), orm.TextOf(b, &m.Name), orm.InputTextOf(b, &m.Tag, func(v string) (string, error) { mutations++; return strings.TrimSpace(v), nil }), orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt), deleted}
	}, orm.WithHooks(func() orm.Hooks[sourceArchive] { callbacks++; return orm.Hooks[sourceArchive]{} }), orm.WithGlobalScopes(func(f sourceArchives) []orm.GlobalScope[sourceArchive] {
		return []orm.GlobalScope[sourceArchive]{orm.Scope(f.Table, "tenant", func(ctx context.Context) (orm.Predicate[sourceArchive], error) {
			v, ok := ctx.Value(writeTenant{}).(int64)
			if !ok {
				return orm.Predicate[sourceArchive]{}, errors.New("missing tenant")
			}
			return orm.NumberEq(f.Tenant, v), nil
		})}
	}))
	_, err = db.Exec(ctx, `INSERT INTO source_entries VALUES(1,10,1,'first'),(2,20,1,'second'),(3,30,2,'other')`)
	must(t, err)
	src := r.From(s.Query().OrderBy(orm.KeyAsc(s.Key())).Limit(3))
	insertion := r.InsertFrom(src, d.Query(), r.MapInsert(orm.KeyExpr(d.ID), expr.Value(orm.NumberExpr(s.Target))), r.MapInsert(orm.NumberExpr(d.Tenant), expr.Value(orm.NumberExpr(s.Tenant))), r.MapInsert(orm.TextExpr(d.Name), expr.Value(orm.TextExpr(s.Name)))).Values(orm.InputTextSet(d.Tag, " trimmed "))
	got, err := insertion.Returning(ctx, db, 3)
	must(t, err)
	if len(got) != 2 || mutations != 1 || callbacks != 0 {
		t.Fatal(len(got), mutations, callbacks)
	}
	for _, m := range got {
		if m.Visits != 7 || m.Tag != "trimmed" || m.CreatedAt.IsZero() || !m.CreatedAt.Equal(m.UpdatedAt) || !m.DeletedAt.IsNull() {
			t.Fatal(m)
		}
	}
	zero, e := r.DeleteUsing(d.Query(), r.From(s.Query())).Match(r.MatchNullableSource(orm.KeyExpr(d.Key()), expr.NullableRow(expr.Value(orm.NumberExpr(s.Target))).Param(value.Null[int64]()))).Exec(ctx, db)
	must(t, e)
	if zero != 0 {
		t.Fatal("NULL source key matched a destination", zero)
	}
	key := r.MatchSource(orm.KeyExpr(d.Key()), expr.Value(orm.NumberExpr(s.Target)))
	update := r.UpdateFrom(d.Query(), r.From(s.Query())).Match(key)
	_, err = db.Exec(ctx, `INSERT INTO source_entries VALUES(4,10,1,'duplicate')`)
	must(t, err)
	for _, returning := range []bool{false, true} {
		mapped := update.Select(r.MapUpdate(orm.TextExpr(d.Name), expr.Value(orm.TextExpr(s.Name))))
		if returning {
			_, err = mapped.Returning(ctx, db, 5)
		} else {
			_, err = mapped.Exec(ctx, db)
		}
		if !errors.Is(err, database.TooManyRows) {
			t.Fatal("ambiguous update accepted", err)
		}
		persisted, e := orm.RequireKey(ctx, db, d.Table, 10)
		must(t, e)
		if persisted.Name != "first" {
			t.Fatal("ambiguous update persisted", persisted)
		}
	}
	_, err = update.Values(orm.TextSet(d.Name, "overflow")).Returning(ctx, db, 1)
	if !errors.Is(err, database.TooManyRows) {
		t.Fatal("overflow accepted", err)
	}
	persisted, e := orm.RequireKey(ctx, db, d.Table, 10)
	must(t, e)
	if persisted.Name != "first" {
		t.Fatal(persisted)
	}
	n, err := update.Values(orm.TextSet(d.Name, "fixed"), orm.InputTextSet(d.Tag, " changed ")).Exec(ctx, db)
	must(t, err)
	if n != 2 {
		t.Fatal(n)
	}
	persisted, e = orm.RequireKey(ctx, db, d.Table, 10)
	must(t, e)
	if persisted.Name != "fixed" || persisted.Tag != "changed" || !persisted.CreatedAt.Equal(got[0].CreatedAt) {
		t.Fatal(persisted)
	}
	stop := errors.New("abort enclosing transaction")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, e := update.Values(orm.TextSet(d.Name, "rollback")).Exec(ctx, tx)
		if e != nil {
			return e
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	persisted, e = orm.RequireKey(ctx, db, d.Table, 10)
	must(t, e)
	if persisted.Name != "fixed" {
		t.Fatal("outer rollback failed", persisted)
	}
	if _, err = update.Values(orm.TextSet(d.Name, "unscoped")).Exec(t.Context(), db); err == nil {
		t.Fatal("missing tenant accepted")
	}
	// Destination scopes apply independently even when source scopes are bypassed.
	_, err = db.Exec(ctx, `INSERT INTO source_archives VALUES(30,2,'other','tag',7,NOW(),NOW(),NULL)`)
	must(t, err)
	n, err = r.UpdateFrom(d.Query(), r.From(orm.WithoutGlobalScopes(s.Query()))).Match(key).Values(orm.TextSet(d.Name, "scoped")).Exec(ctx, db)
	must(t, err)
	if n != 2 {
		t.Fatal(n)
	}
	other, e := orm.All(context.WithValue(ctx, writeTenant{}, int64(2)), db, d.Query().Where(orm.KeyEq(d.Key(), 30)).WithTrashed())
	must(t, e)
	if len(other) != 1 || other[0].Name != "other" {
		t.Fatal(other)
	}
	removed, err := r.DeleteUsing(d.Query(), r.From(s.Query())).Match(key).Returning(ctx, db, 3)
	must(t, err)
	if len(removed) != 2 || removed[0].DeletedAt.IsNull() {
		t.Fatal(removed)
	}
	n, err = r.ForceDeleteUsing(d.Query(), d.Soft, r.From(s.Query())).Match(key).Exec(ctx, db)
	must(t, err)
	if n != 0 {
		t.Fatal("force delete lost active visibility", n)
	}
	n, err = r.ForceDeleteUsing(d.Query().WithTrashed(), d.Soft, r.From(s.Query())).Match(key).Exec(ctx, db)
	must(t, err)
	if n != 2 {
		t.Fatal(n)
	}
	remaining, e := orm.All(ctx, db, d.Query().WithTrashed())
	must(t, e)
	if len(remaining) != 0 || callbacks != 0 {
		t.Fatal(remaining, callbacks)
	}
}
