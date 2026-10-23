package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	j "foundry.test/goway/models/joined"
	old "foundry.test/goway/oldmodels/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func pivotWriteReply(s string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
	if strings.HasPrefix(s, "SELECT EXISTS(") {
		return []string{"exists"}, [][]driver.Value{{true}}
	}
	if strings.HasPrefix(s, "SELECT COUNT(*) FROM (") {
		return []string{"count"}, [][]driver.Value{{int64(1)}}
	}
	if strings.Contains(s, `FROM "joined_parents"`) {
		return make([]string, 3), [][]driver.Value{{int64(1), "parent", nil}}
	}
	if strings.Contains(s, `FROM "joined_targets"`) {
		return make([]string, 5), [][]driver.Value{{int64(10), int64(1), "target", int64(7), nil}}
	}
	role := "member"
	if strings.HasPrefix(s, `UPDATE`) {
		role = "new"
	}
	return make([]string, 6), [][]driver.Value{{int64(100), int64(1), int64(10), "parent", role, nil}}
}
func TestRelationWriteGoldens(t *testing.T) {
	for _, mode := range []string{"single_attach", "attach", "sync", "keep", "toggle", "detach_many", "detach_all", "update"} {
		t.Run(mode, func(t *testing.T) {
			captures := []*goldenCapture{{reply: pivotWriteReply}, {reply: pivotWriteReply}}
			for side, g := range captures {
				db := g.open(t.Context())
				var err error
				if side == 0 {
					r := old.ParentRelations().Peers.Where(old.TargetFields().Rank.Gt(0)).WherePivot(old.PivotFields().Kind.Eq("parent"))
					draft := old.PivotDraft{}.SetID(100).SetKind("parent").SetRole("member")
					update := old.PivotDraft{}.SetRole("new")
					switch mode {
					case "single_attach":
						out, e := r.Attach(t.Context(), db, old.Parent{ID: 1}, old.Target{ID: 10}, draft)
						err = e
						g.show(out)
					case "attach":
						out, e := r.AttachMany(t.Context(), db, old.Parent{ID: 1}, []old.Target{{ID: 10}}, draft)
						err = e
						g.show(out)
					case "sync":
						out, e := r.Sync(t.Context(), db, old.Parent{ID: 1}, []old.Target{{ID: 10}}, draft, update)
						err = e
						g.show(out.Updated)
					case "keep":
						out, e := r.SyncWithoutDetaching(t.Context(), db, old.Parent{ID: 1}, []old.Target{{ID: 10}}, draft, update)
						err = e
						g.show(out.Updated)
					case "toggle":
						out, e := r.Toggle(t.Context(), db, old.Parent{ID: 1}, []old.Target{{ID: 10}}, draft)
						err = e
						g.show(out.Detached)
					case "detach_many":
						out, e := r.DetachMany(t.Context(), db, old.Parent{ID: 1}, []old.Target{{ID: 10}})
						err = e
						g.show(out)
					case "detach_all":
						out, e := r.DetachAll(t.Context(), db, old.Parent{ID: 1})
						err = e
						g.show(out)
					case "update":
						out, e := r.UpdateExistingPivot(t.Context(), db, old.Parent{ID: 1}, old.Target{ID: 10}, update)
						err = e
						g.show(out)
					}
				} else {
					r := j.Peers.Where(j.Targets.Rank.Gt(0)).WherePivot(j.Pivots.Kind.Eq("parent"))
					create := []orm.Assignment[j.Pivot]{j.Pivots.ID.Set(100), j.Pivots.Kind.Set("parent"), j.Pivots.Role.Set("member")}
					update := []orm.Assignment[j.Pivot]{j.Pivots.Role.Set("new")}
					switch mode {
					case "single_attach":
						out, e := orm.Attach(t.Context(), db, r, j.Parent{ID: 1}, j.Target{ID: 10}, create...)
						err = e
						g.show(out)
					case "attach":
						out, e := orm.AttachMany(t.Context(), db, r, j.Parent{ID: 1}, []j.Target{{ID: 10}}, create...)
						err = e
						g.show(out)
					case "sync":
						out, e := orm.Sync(t.Context(), db, r, j.Parent{ID: 1}, []j.Target{{ID: 10}}, create, update)
						err = e
						g.show(out.Updated)
					case "keep":
						out, e := orm.SyncWithoutDetaching(t.Context(), db, r, j.Parent{ID: 1}, []j.Target{{ID: 10}}, create, update)
						err = e
						g.show(out.Updated)
					case "toggle":
						out, e := orm.Toggle(t.Context(), db, r, j.Parent{ID: 1}, []j.Target{{ID: 10}}, create...)
						err = e
						g.show(out.Detached)
					case "detach_many":
						out, e := orm.DetachMany(t.Context(), db, r, j.Parent{ID: 1}, []j.Target{{ID: 10}})
						err = e
						g.show(out)
					case "detach_all":
						out, e := orm.DetachAll(t.Context(), db, r, j.Parent{ID: 1})
						err = e
						g.show(out)
					case "update":
						out, e := orm.UpdatePivot(t.Context(), db, r, j.Parent{ID: 1}, j.Target{ID: 10}, update...)
						err = e
						g.show(out)
					}
				}
				if err != nil {
					t.Fatal(side, err, g.output.String())
				}
				want := 4
				if mode == "detach_all" {
					want = 3
				}
				if g.statements != want {
					t.Fatal("statements", side, g.statements, want)
				}
				if err = db.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if captures[0].output.String() != captures[1].output.String() {
				t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
			}
		})
	}
}
func TestRelationWriteMorphDefaultsAndSingleStatements(t *testing.T) {
	for _, mode := range []string{"attach", "detach", "force", "morph"} {
		t.Run(mode, func(t *testing.T) {
			g := &goldenCapture{reply: pivotWriteReply}
			db := g.open(t.Context())
			defer db.Close(context.Background())
			var err error
			switch mode {
			case "attach":
				_, err = orm.Attach(t.Context(), db, j.Peers, j.Parent{ID: 1}, j.Target{ID: 10}, j.Pivots.ID.Set(100), j.Pivots.Kind.Set("parent"), j.Pivots.Role.Set("member"))
			case "morph":
				_, err = orm.Attach(t.Context(), db, j.MorphPeers, j.Parent{ID: 1}, j.Target{ID: 10}, j.Pivots.ID.Set(100), j.Pivots.Role.Set("member"))
			case "detach":
				_, err = orm.Detach(t.Context(), db, j.Peers, j.Parent{ID: 1}, j.Target{ID: 10})
			case "force":
				_, err = orm.ForceDetach(t.Context(), db, j.Peers.WithTrashedPivot(), j.Parent{ID: 1}, j.Target{ID: 10})
			}
			if err != nil {
				t.Fatal(err, g.output.String())
			}
			want := 3
			if mode == "attach" || mode == "morph" {
				want = 4
			}
			if g.statements != want {
				t.Fatal(g.statements, want)
			}
			if mode == "morph" && !strings.Contains(g.output.String(), `"parent"`) {
				t.Fatal("missing morph default")
			}
		})
	}
}
