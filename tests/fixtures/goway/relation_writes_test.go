package goway_test

import (
	"context"
	"database/sql/driver"
	j "foundry.test/goway/models/joined"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"strings"
	"testing"
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
			captures := []*goldenCapture{{reply: pivotWriteReply}}
			for _, g := range captures {
				db := g.open(t.Context())
				var err error

				r := j.Peers.Where(orm.NumberGt(j.Targets.Rank, 0)).WherePivot(orm.ColumnEq(j.Pivots.Kind, "parent"))
				create := []orm.Assignment[j.Pivot]{orm.KeySet(j.Pivots.ID, 100), orm.ColumnSet(j.Pivots.Kind, "parent"), orm.TextSet(j.Pivots.Role, "member")}
				update := []orm.Assignment[j.Pivot]{orm.TextSet(j.Pivots.Role, "new")}
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

				if err != nil {
					t.Fatal(err, g.output.String())
				}
				want := 4
				if mode == "detach_all" {
					want = 3
				}
				if g.statements != want {
					t.Fatal("statements", g.statements, want)
				}
				if err = db.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if !oracleEqual(t, "relation_writes/1", captures[0].output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}
