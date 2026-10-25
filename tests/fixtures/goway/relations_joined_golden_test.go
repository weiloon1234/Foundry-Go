package goway_test

import (
	"context"
	"database/sql/driver"
	j "foundry.test/goway/models/joined"
	old "foundry.test/goway/oldmodels/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
)

var _ = j.Parents

func joinedReply(sql string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
	target := []driver.Value{int64(10), int64(1), "ten", int64(7), nil}
	pivot := []driver.Value{int64(100), int64(1), int64(10), "parent", "admin", nil}
	if strings.HasPrefix(sql, "SELECT COUNT") {
		return []string{"count"}, [][]driver.Value{{int64(1)}}
	}
	if strings.Contains(sql, `AS "foundry_target"`) {
		if strings.Contains(sql, `FROM "joined_parents"`) {
			return make([]string, 9), [][]driver.Value{append([]driver.Value{int64(1), "one", nil}, pivot...)}
		}
		return make([]string, 11), [][]driver.Value{append(target, pivot...)}
	}
	if strings.Contains(sql, `AS "foundry_through"`) {
		return make([]string, 6), [][]driver.Value{append(target, int64(1))}
	}
	if strings.Contains(sql, `FROM "joined_targets"`) {
		return []string{"id", "parent_id", "name", "rank", "deleted_at"}, [][]driver.Value{target}
	}
	if strings.Contains(sql, `FROM "joined_pivots"`) {
		return []string{"id", "parent_id", "target_id", "kind", "role", "deleted_at"}, [][]driver.Value{pivot}
	}
	return []string{"id", "name", "deleted_at"}, [][]driver.Value{{int64(1), "one", nil}}
}
func TestJoinedFamiliesSQLMatchesOldAPI(t *testing.T) {
	ctx := context.Background()
	t.Run("pivot", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.Peers.Where(tf.Rank.Gt(0)).WherePivot(vf.Role.Eq("admin")).OrderByPivot(vf.Role.Desc()).OrderBy(tf.Name.Asc()).With(tr.Parent).WithPivot(vr.Parent)).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.Peers.Where(j.Targets.Rank.Gt(0)).WherePivot(j.Pivots.Role.Eq("admin")).OrderByPivot(j.Pivots.Role.Desc()).OrderBy(j.Targets.Name.Asc()).With(j.TargetParent).WithPivot(j.PivotParent))
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 3 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/1", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("through_many", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.Targets).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.TargetsThrough)
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/2", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("through_one", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.ChosenThrough).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				for _, parent := range hydrated {
					g.show(parent.ID)
					g.showHydrated(parent.ChosenThrough)
				}
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.ChosenThrough)
				if err != nil {
					t.Fatal(err)
				}
				for _, parent := range hydrated {
					g.show(parent.ID)
					g.showHydrated(parent.Chosen)
				}
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/3", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("of_many", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.Chosen.OfMany(tf.Rank.Desc())).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.Chosen.OfMany(j.Targets.Rank.Desc()))
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/4", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("latest", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.Chosen.LatestOfMany()).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.Chosen.LatestOfMany())
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/5", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("oldest", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.Chosen.OldestOfMany()).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.Chosen.OldestOfMany())
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/6", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("through_latest", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.ChosenThrough.LatestOfMany()).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				for _, parent := range hydrated {
					g.show(parent.ID)
					g.showHydrated(parent.ChosenThrough)
				}
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.ChosenThrough.LatestOfMany())
				if err != nil {
					t.Fatal(err)
				}
				for _, parent := range hydrated {
					g.show(parent.ID)
					g.showHydrated(parent.Chosen)
				}
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/7", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("morph_many", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.MorphChildren).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.MorphChildren)
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/8", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("morph_one", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(pr.MorphChild).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.MorphChild)
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/9", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})
	t.Run("morph_pivot", func(t *testing.T) {
		captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
		for side, g := range captures {
			db := g.open(ctx)
			if side == 0 {
				pf, tf, vf := old.ParentFields(), old.TargetFields(), old.PivotFields()
				pr, tr, vr := old.ParentRelations(), old.TargetRelations(), old.PivotRelations()
				_, _, _, _, _, _ = pf, tf, vf, pr, tr, vr
				hydrated, err := old.QueryJoinedParents().With(query.MorphToMany(pf.ID, vf.ParentID, vf.Kind, vf.TargetID, tf.ID).Bind("Peers", old.QueryJoinedParents().Query, old.QueryJoinedTargets().Query, old.QueryJoinedPivots().Query, func(m old.Parent) relation.Through[old.Target, old.Pivot] { return m.Peers }, func(m old.Parent, v relation.Through[old.Target, old.Pivot]) old.Parent { m.Peers = v; return m })).Load(ctx, db, []old.Parent{{ID: 1}, {ID: 2}})
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			} else {
				hydrated, err := orm.Load(ctx, db, []j.Parent{{ID: 1}, {ID: 2}}, j.MorphPeers)
				if err != nil {
					t.Fatal(err)
				}
				g.showHydrated(hydrated)
			}
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if g.statements != 1 {
				t.Fatal("statement count", g.statements)
			}
		}
		if !oracleEqual(t, "relations_joined_golden/10", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	})

	for _, inverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "morph_to", true: "morph_inverse"}[inverse], func(t *testing.T) {
			captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
			for side, g := range captures {
				db := g.open(ctx)
				if side == 0 {
					if inverse {
						hydrated, err := old.QueryJoinedTargets().With(old.TargetRelations().Inverse).Load(ctx, db, []old.Target{{ID: 10}})
						if err != nil {
							t.Fatal(err)
						}
						g.showHydrated(hydrated)
					} else {
						r := old.PivotRelations()
						hydrated, err := old.QueryJoinedPivots().With(r.MorphParent, r.MorphTarget).Load(ctx, db, []old.Pivot{{ID: 100, ParentID: value.Of(int64(1)), Kind: "parent"}, {ID: 101, ParentID: value.Of(int64(10)), Kind: "target"}})
						if err != nil {
							t.Fatal(err)
						}
						g.showHydrated(hydrated)
					}
				} else {
					if inverse {
						hydrated, err := orm.Load(ctx, db, []j.Target{{ID: 10}}, j.Inverse)
						if err != nil {
							t.Fatal(err)
						}
						g.showHydrated(hydrated)
					} else {
						hydrated, err := orm.Load(ctx, db, []j.Pivot{{ID: 100, ParentID: value.Of(int64(1)), Kind: "parent"}, {ID: 101, ParentID: value.Of(int64(10)), Kind: "target"}}, j.MorphParent, j.MorphTarget)
						if err != nil {
							t.Fatal(err)
						}
						g.showHydrated(hydrated)
					}
				}
				if err := db.Close(ctx); err != nil {
					t.Fatal(err)
				}
				want := 2
				if inverse {
					want = 1
				}
				if g.statements != want {
					t.Fatal("statements", g.statements)
				}
			}
			if !oracleEqual(t, "relations_joined_golden/11", captures[0].output.String(), captures[1].output.String()) {
				t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
			}
		})
	}
}

func TestJoinedExistenceSQLMatchesOldAPI(t *testing.T) {
	ctx := context.Background()
	for _, family := range []string{"pivot", "hop", "morph_pivot", "morph_to"} {
		t.Run(family, func(t *testing.T) {
			captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
			for side, g := range captures {
				db := g.open(ctx)
				var err error
				if side == 0 {
					p, tg, v := old.ParentFields(), old.TargetFields(), old.PivotFields()
					pr := old.ParentRelations()
					switch family {
					case "pivot":
						_, err = old.QueryJoinedParents().WhereHas(pr.Peers.Where(tg.Rank.Gt(1)).WherePivot(v.Role.Eq("admin"))).Count(ctx, db)
					case "hop":
						_, err = old.QueryJoinedParents().WhereDoesntHave(pr.Targets.Where(tg.Rank.Gt(1))).Count(ctx, db)
					case "morph_pivot":
						r := query.MorphToMany(p.ID, v.ParentID, v.Kind, v.TargetID, tg.ID).Bind("Peers", old.QueryJoinedParents().Query, old.QueryJoinedTargets().Query, old.QueryJoinedPivots().Query, func(m old.Parent) relation.Through[old.Target, old.Pivot] { return m.Peers }, func(m old.Parent, r relation.Through[old.Target, old.Pivot]) old.Parent { m.Peers = r; return m })
						_, err = old.QueryJoinedParents().WhereHas(r).Count(ctx, db)
					case "morph_to":
						_, err = old.QueryJoinedPivots().WhereHas(old.PivotRelations().MorphParent).Count(ctx, db)
					}
				} else {
					switch family {
					case "pivot":
						_, err = j.Parents.Query().Where(orm.Has(j.Peers.WherePivot(j.Pivots.Role.Eq("admin")), j.Targets.Rank.Gt(1))).Count(ctx, db)
					case "hop":
						_, err = j.Parents.Query().Where(orm.DoesntHave(j.TargetsThrough, j.Targets.Rank.Gt(1))).Count(ctx, db)
					case "morph_pivot":
						_, err = j.Parents.Query().Where(orm.Has(j.MorphPeers)).Count(ctx, db)
					case "morph_to":
						_, err = j.Pivots.Query().Where(orm.Has(j.MorphParent)).Count(ctx, db)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Close(ctx); err != nil {
					t.Fatal(err)
				}
				if g.statements != 1 {
					t.Fatal("existence statement count", g.statements)
				}
			}
			if !oracleEqual(t, "relations_joined_golden/12", captures[0].output.String(), captures[1].output.String()) {
				t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
			}
		})
	}
}
