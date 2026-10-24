package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	e "foundry.test/goway/models/extensions"
	n "foundry.test/goway/models/hooks"
	o "foundry.test/goway/oldmodels/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func TestORMExtensionDeclaration(t *testing.T) {
	if err := orm.Check(e.Articles, e.Authors); err != nil {
		t.Fatal(err)
	}
	if err := e.Owner.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := e.Declaration().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, slot := range e.ArticleExtensions.All() {
		g := &goldenCapture{}
		db := readDB(t, g)
		if _, err := orm.Load(t.Context(), db, []e.Article{{ID: 1}}, slot); !errors.Is(err, fault.Missing) {
			t.Fatal("unbound runtime", err)
		}
		if g.statements != 0 {
			t.Fatal("unbound loader executed SQL")
		}
	}
	other := orm.IdentityOf(e.Articles.Table, e.Articles.Key())
	pinned := extensions.DefineORMOwner("articles", other, extensions.OwnerOptions{StorageModel: "extension_articles"})
	a, err := e.Owner.Subject(e.Owner.Reference(1))
	if err != nil {
		t.Fatal(err)
	}
	b, err := pinned.Subject(other.ReferenceOf(e.Article{ID: 1}))
	if err != nil || a.Key != b.Key || a.Scope != b.Scope {
		t.Fatal("persisted owner identity", err)
	}
}

func TestFactoryORMAndGeneratedParity(t *testing.T) {
	for _, count := range []int{1, 3} {
		for _, after := range []bool{false, true} {
			var transcripts []string
			var traces [][]string
			var statements []int
			for _, old := range []bool{true, false} {
				var trace []string
				ctx := coremodels.LifecycleContext(t.Context(), &trace)
				now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
				g := &goldenCapture{reply: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value) {
					return []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, [][]driver.Value{{args[0].Value, "stored", nil, []byte{1}, now, now, nil}}
				}}
				db := g.open(ctx)
				hook := func() { trace = append(trace, "factory.after") }
				if old {
					f, err := factory.New[o.Member](func(_ context.Context, n factory.Sequence) (o.MemberDraft, error) {
						return (o.MemberDraft{}).SetID(int64(n)).SetName(" input ").SetBody([]byte{1}), nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if after {
						f, err = f.AfterCreating(func(_ context.Context, _ database.Transactor, m o.Member) (o.Member, error) { hook(); return m, nil })
						if err != nil {
							t.Fatal(err)
						}
					}
					if count == 1 {
						m, err := f.Create(ctx, db)
						if err != nil {
							t.Fatal(err)
						}
						g.show(m)
					} else {
						m, err := f.CreateMany(ctx, db, count)
						if err != nil {
							t.Fatal(err)
						}
						g.show(m)
					}
				} else {
					f, err := factory.Define(n.Members.Table, func(_ context.Context, seq factory.Sequence) ([]orm.Assignment[n.Member], error) {
						return []orm.Assignment[n.Member]{n.Members.Key().Set(int64(seq)), n.Members.Name.Set(" input "), n.Members.Body.Set([]byte{1})}, nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if after {
						f, err = f.AfterCreating(func(_ context.Context, _ database.Transactor, m n.Member) (n.Member, error) { hook(); return m, nil })
						if err != nil {
							t.Fatal(err)
						}
					}
					if count == 1 {
						m, err := f.Create(ctx, db)
						if err != nil {
							t.Fatal(err)
						}
						g.show(m)
					} else {
						m, err := f.CreateMany(ctx, db, count)
						if err != nil {
							t.Fatal(err)
						}
						g.show(m)
					}
				}
				if err := db.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				transcripts = append(transcripts, g.output.String())
				traces = append(traces, trace)
				statements = append(statements, g.statements)
			}
			if transcripts[0] != transcripts[1] || !reflect.DeepEqual(traces[0], traces[1]) || statements[0] != statements[1] {
				t.Fatalf("factory parity count=%d after=%t\nold %s %v\nnew %s %v", count, after, transcripts[0], traces[0], transcripts[1], traces[1])
			}
			expected := count
			if after {
				expected += 2
			}
			if statements[1] != expected {
				t.Fatal("factory statement count", statements, expected)
			}
		}
	}
}

func TestORMModelBindingScopeAndDuplicateParity(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		var transcripts []string
		var failures []bool
		for _, old := range []bool{true, false} {
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			rows := make([][]driver.Value, count)
			for i := range rows {
				rows[i] = []driver.Value{int64(1), "visible", nil, []byte{1}, now, now, nil}
			}
			g := &goldenCapture{scriptedColumns: []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, scriptedBatches: [][][]driver.Value{rows}}
			db := readDB(t, g)
			var err error
			if old {
				r := modelbinding.ByField(db, o.QueryHookMembers().Where(o.MemberFields().ID.Eq(1)), o.MemberFields().Name, func(string) string { return "visible" })
				_, err = r.Resolve(t.Context(), "visible")
			} else {
				r := modelbinding.ByColumn(db, n.Members.Query().Where(n.Members.Key().Eq(1)), n.Members.Name, func(string) string { return "visible" })
				_, err = r.Resolve(t.Context(), "visible")
			}
			transcripts = append(transcripts, g.output.String())
			failures = append(failures, errors.Is(err, fault.Internal))
			if g.statements != 1 {
				t.Fatal("binding statement count", g.statements)
			}
		}
		if transcripts[0] != transcripts[1] || failures[0] != failures[1] {
			t.Fatal("binding parity", transcripts, failures)
		}
	}
}
