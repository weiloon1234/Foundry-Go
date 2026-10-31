package recursivequeries_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/recursivequeries"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type parentAlias struct{}
type childAlias struct{}
type resultAlias struct{}

func descendants(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
	parent := rowset.AsRecord[parentAlias](self, "parent")
	child := rowset.As[childAlias](models.Users.Query(), "child")
	p, c := rowset.Scope(parent), rowset.Scope(child)
	joined := rowset.InnerJoin(child, parent, rowset.OnEqual(expr.At(c, orm.NullableColumnExpr(models.Users.IntroducerID)), expr.NullableRow(expr.At(p, orm.KeyExpr(models.Users.ID)))))
	return rowset.SelectRecord(joined, rowset.LeftScope(joined, rowset.Scope(child)))
}

func readUsers(t *testing.T, tx database.Executor, definition rowset.CommonTable[models.User]) []models.User {
	t.Helper()
	source := rowset.AsRecord[resultAlias](definition, "result")
	fields := rowset.Scope(source)
	rows, err := rowset.ReadRecord(rowset.SelectRecord(source, rowset.Scope(source)).OrderBy(expr.Asc(expr.At(fields, orm.TextExpr(models.Users.Email))))).All(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestPostgresRecursiveModelHierarchy(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		ctx := t.Context()
		// Extend the existing a->b relation to a->b->c using a typed write.
		var err error
		users[2], err = orm.Update(ctx, tx, models.Users.Query(), models.Users.Key(), users[2].ID, orm.NullableColumnSet(models.Users.IntroducerID, users[1].ID))
		if err != nil {
			return err
		}
		u := models.Users
		anchor := models.Users.Query().Where(orm.KeyEq(u.ID, users[0].ID))
		tree := rowset.RecursiveCTE("descendants", rowset.ModelRecord(anchor), descendants)
		if rows := readUsers(t, tx, tree); !reflect.DeepEqual(rows, users) {
			t.Fatal("recursive model hierarchy lost complete records or levels")
		}
		if rows := readUsers(t, tx, tree.Materialized()); !reflect.DeepEqual(rows, users) {
			t.Fatal("materialization changed recursion")
		}
		if rows := readUsers(t, tx, rowset.RecursiveCTE("empty_tree", rowset.ModelRecord(anchor.Where(orm.NumberLt(u.Age, 0))), descendants)); len(rows) != 0 {
			t.Fatal("empty recursive anchor produced rows")
		}
		pruned := rowset.RecursiveCTE("pruned", rowset.ModelRecord(anchor), func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			parent := rowset.AsRecord[parentAlias](self, "parent")
			child := rowset.As[childAlias](models.Users.Query().Where(orm.NumberLt(u.Age, 22)), "child")
			j := rowset.InnerJoin(child, parent, rowset.OnEqual(expr.At(rowset.Scope(child), orm.NullableColumnExpr(models.Users.IntroducerID)), expr.NullableRow(expr.At(rowset.Scope(parent), orm.KeyExpr(models.Users.ID)))))
			return rowset.SelectRecord(j, rowset.LeftScope(j, rowset.Scope(child)))
		})
		if rows := readUsers(t, tx, pruned); !reflect.DeepEqual(rows, users[:2]) {
			t.Fatal("recursive step predicate did not prune descendants")
		}
		roots := models.Users.Query().Where(orm.NumberLte(u.Age, 21))
		if rows := readUsers(t, tx, rowset.RecursiveCTE("distinct_paths", rowset.ModelRecord(roots), descendants)); !reflect.DeepEqual(rows, users) {
			t.Fatal("UNION did not deduplicate recursive paths")
		}
		if rows := readUsers(t, tx, rowset.RecursiveAllCTE("all_paths", rowset.ModelRecord(roots), descendants)); !reflect.DeepEqual(rows, []models.User{users[0], users[1], users[1], users[2], users[2]}) {
			t.Fatal("UNION ALL lost duplicate paths")
		}
		if err := checkRecursiveComposition(t, tx, tree, users); err != nil {
			return err
		}
		if err := checkRecursiveProjection(t, tx, anchor, users); err != nil {
			return err
		}
		if err := checkPermittedStepSources(t, tx, anchor); err != nil {
			return err
		}
		// Close the cycle c->a. UNION of unchanged complete records terminates.
		users[0], err = orm.Update(ctx, tx, models.Users.Query(), models.Users.Key(), users[0].ID, orm.NullableColumnSet(models.Users.IntroducerID, users[2].ID))
		if err != nil {
			return err
		}
		if rows := readUsers(t, tx, tree); !reflect.DeepEqual(rows, users) {
			t.Fatal("recursive UNION did not terminate a repeated-record cycle")
		}
		return nil
	})
}

func checkPermittedStepSources(t *testing.T, tx *database.Tx, anchor orm.Query[models.User]) error {
	for name, step := range map[string]func(rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User]{
		"intersection": func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			return rowset.Intersect(self, rowset.ModelRecord(models.Users.Query()))
		},
		"left_except": func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			return rowset.Except(self, rowset.ModelRecord(models.Users.Query().Limit(0)))
		},
		"union_step": func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			return rowset.UnionAll(self, rowset.ModelRecord(models.Users.Query().Limit(0)))
		},
		"preserved_left": func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			a, b := rowset.AsRecord[parentAlias](self, "a"), rowset.As[childAlias](models.Users.Query().Limit(0), "b")
			j := rowset.LeftJoin(a, b, rowset.On(expr.At(rowset.Scope(a), orm.KeyExpr(models.Users.ID)), expr.At(rowset.Scope(b), orm.KeyExpr(models.Users.ID))))
			return rowset.SelectRecord(j, rowset.LeftScope(j, rowset.Scope(a)))
		},
		"preserved_right": func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			a, b := rowset.As[parentAlias](models.Users.Query().Limit(0), "a"), rowset.AsRecord[childAlias](self, "b")
			j := rowset.RightJoin(a, b, rowset.On(expr.At(rowset.Scope(a), orm.KeyExpr(models.Users.ID)), expr.At(rowset.Scope(b), orm.KeyExpr(models.Users.ID))))
			return rowset.SelectRecord(j, rowset.RightScope(j, rowset.Scope(b)))
		},
		"independent_aggregate": func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
			a := rowset.AsRecord[parentAlias](self, "a")
			count := rowset.SelectScalar(rowset.From(models.Orders.Query()), expr.AsSelected(expr.Count[models.Order]()))
			return rowset.SelectRecord(a, rowset.Scope(a)).Where(rowset.ExistsQuery(a, count))
		},
	} {
		tree := rowset.RecursiveCTE(name, rowset.ModelRecord(anchor), step)
		if rows := readUsers(t, tx, tree); len(rows) != 1 {
			t.Fatal("permitted recursive step changed repeated anchor", name)
		}
	}
	return nil
}

func checkRecursiveComposition(t *testing.T, tx *database.Tx, tree rowset.CommonTable[models.User], users []models.User) error {
	ctx := t.Context()
	combined := rowset.Union(tree, tree)
	fields := combined.Scope()
	if n, err := rowset.ReadSet(combined).Count(ctx, tx); err != nil || n != 3 {
		t.Fatal("shared recursive CTE/set composition failed", err)
	}
	s, err := rowset.CompileRead(rowset.ReadSet(combined))
	if err != nil {
		return err
	}
	if strings.Count(s.SQL(), `"descendants" ("id"`) != 1 {
		t.Fatal("shared recursive CTE was emitted more than once")
	}
	ids := rowset.SelectScalar(combined, expr.AsSelected(expr.Row(expr.At(fields, orm.KeyExpr(models.Users.ID)))))
	if n, err := orm.Count(ctx, tx, models.Users.Query().Where(orm.Has(models.UserLinks.Referrals.Where(expr.RelationFilter(models.UserLinks.Referrals, rowset.InQuery(expr.Value(orm.KeyExpr(models.Users.ID)), ids)))))); err != nil || n != 2 {
		t.Fatal("relationship predicate lost recursive CTE dependency", err)
	}
	if n, err := orm.Count(ctx, tx, expr.Where(models.Users.Query(), rowset.InQuery(expr.Value(orm.KeyExpr(models.Users.ID)), ids))); err != nil || n != 3 {
		t.Fatal("recursive membership failed", err)
	}
	result := rowset.AsRecord[resultAlias](tree, "result")
	f := rowset.Scope(result)
	q := rowset.SelectRecord(result, rowset.Scope(result)).OrderBy(expr.Asc(expr.At(f, orm.TextExpr(models.Users.Email))))
	if row, err := rowset.ReadRecord(q.Offset(1)).RequireFirst(ctx, tx); err != nil || row.ID != users[1].ID {
		t.Fatal("recursive required result lost selected window", err)
	}
	if row, err := rowset.ReadRecord(q.Limit(0)).First(ctx, tx); err != nil || row.IsSet() {
		t.Fatal("recursive optional result ignored zero limit", err)
	}
	stop := errors.New("stop recursive stream")
	if err := rowset.EachRead(ctx, tx, q, func(models.User) error { return stop }); !errors.Is(err, stop) {
		t.Fatal("recursive stream lost callback failure", err)
	}
	if n, err := rowset.ReadRecord(q).Count(ctx, tx); err != nil || n != 3 {
		t.Fatal("recursive stream failed to release rows", err)
	}
	return nil
}

func checkRecursiveProjection(t *testing.T, tx *database.Tx, anchor orm.Query[models.User], users []models.User) error {
	u := models.Users
	initial := rowset.Select(rowset.From(anchor), reports.UserSummaries.Row,
		rowset.Map(reports.UserSummaries.ID, expr.Value(orm.KeyExpr(u.ID))),
		rowset.Map(reports.UserSummaries.Email, expr.Value(orm.TextExpr(u.Email))),
		rowset.Map(reports.UserSummaries.Nickname, expr.Value(orm.NullableTextExpr(u.Nickname))),
		rowset.Map(reports.UserSummaries.Status, expr.Value(orm.EnumExpr(u.Status))),
	)
	tree := rowset.RecursiveCTE("summaries", rowset.RecordOf(initial), func(self rowset.RecursiveSelf[reports.UserSummary]) rowset.RecordSource[reports.UserSummary] {
		parent := rowset.AsRecord[parentAlias](self, "parent")
		child := rowset.As[childAlias](models.Users.Query(), "child")
		j := rowset.InnerJoin(child, parent, rowset.OnEqual(expr.At(rowset.Scope(child), orm.NullableColumnExpr(models.Users.IntroducerID)), expr.NullableRow(rowset.At(rowset.Scope(parent), reports.UserSummaries.ID))))
		f := rowset.LeftScope(j, rowset.Scope(child))
		return rowset.RecordOf(rowset.Select(j, reports.UserSummaries.Row,
			rowset.Map(reports.UserSummaries.ID, expr.At(f, orm.KeyExpr(models.Users.ID))),
			rowset.Map(reports.UserSummaries.Email, expr.At(f, orm.TextExpr(models.Users.Email))),
			rowset.Map(reports.UserSummaries.Nickname, expr.At(f, orm.NullableTextExpr(models.Users.Nickname))),
			rowset.Map(reports.UserSummaries.Status, expr.At(f, orm.EnumExpr(models.Users.Status))),
		))
	})
	a := rowset.AsRecord[resultAlias](tree, "result")
	f := rowset.Scope(a)
	rows, err := rowset.ReadRecord(rowset.SelectRecord(a, rowset.Scope(a)).OrderBy(expr.Asc(rowset.At(f, reports.UserSummaries.Email)))).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 3 || rows[1].ID != users[1].ID || rows[1].Email != users[1].Email || rows[1].Nickname != users[1].Nickname || rows[1].Status != users[1].Status {
		t.Fatal("recursive projection lost declared aliases, nullable values or codecs")
	}
	return nil
}

func TestPostgresNaturalKeyRecursionAndCancellation(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Session(t.Context(), func(session *database.Session) error {
		session.Discard() // Search path is confined to this dedicated session.
		for _, sql := range []string{`SET search_path TO "` + namespace + `"`, `CREATE TABLE nodes (code text PRIMARY KEY,parent_code text,name text NOT NULL)`} {
			if _, err := session.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		for _, draft := range [][]orm.Assignment[recursivequeries.Node]{
			{orm.TextSet(recursivequeries.Nodes.Code, "root"), orm.TextSet(recursivequeries.Nodes.Name, "Root")},
			{orm.TextSet(recursivequeries.Nodes.Code, "child"), orm.NullableTextSet(recursivequeries.Nodes.Parent, recursivequeries.NodeCode("root")), orm.TextSet(recursivequeries.Nodes.Name, "Child")},
			{orm.TextSet(recursivequeries.Nodes.Code, "leaf"), orm.NullableTextSet(recursivequeries.Nodes.Parent, recursivequeries.NodeCode("child")), orm.TextSet(recursivequeries.Nodes.Name, "Leaf")},
		} {
			if _, err := orm.Insert(t.Context(), session, recursivequeries.Nodes.Table, draft...); err != nil {
				return err
			}
		}
		n := recursivequeries.Nodes
		anchor := recursivequeries.Nodes.Query().Where(orm.TextEq(n.Code, "root"))
		tree := rowset.RecursiveCTE("node_tree", rowset.ModelRecord(anchor), func(self rowset.RecursiveSelf[recursivequeries.Node]) rowset.RecordSource[recursivequeries.Node] {
			parent := rowset.AsRecord[parentAlias](self, "parent")
			// Derived self-reference remains a FROM source, not an expression subexpr.
			working := rowset.AsRecord[resultAlias](rowset.SelectRecord(parent, rowset.Scope(parent)), "working")
			child := rowset.As[childAlias](recursivequeries.Nodes.Query(), "child")
			j := rowset.InnerJoin(child, working, rowset.OnEqual(expr.At(rowset.Scope(child), orm.NullableTextExpr(recursivequeries.Nodes.Parent)), expr.NullableRow(expr.At(rowset.Scope(working), orm.TextExpr(recursivequeries.Nodes.Code)))))
			return rowset.SelectRecord(j, rowset.LeftScope(j, rowset.Scope(child)))
		})
		a := rowset.AsRecord[resultAlias](tree, "result")
		fields := rowset.Scope(a)
		codes, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(a, expr.AsSelected(expr.Row(expr.At(fields, orm.TextExpr(recursivequeries.Nodes.Code))))), expr.Asc(expr.At(fields, orm.TextExpr(recursivequeries.Nodes.Code))))).All(t.Context(), session)
		if err != nil || !reflect.DeepEqual(codes, []recursivequeries.NodeCode{"child", "leaf", "root"}) {
			t.Fatal("recursive natural-key hierarchy failed", codes, err)
		}
		loop := rowset.RecursiveAllCTE("nonterminating", rowset.ModelRecord(anchor), func(self rowset.RecursiveSelf[recursivequeries.Node]) rowset.RecordSource[recursivequeries.Node] {
			return self
		})
		source := rowset.AsRecord[resultAlias](loop, "result")
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		_, err = rowset.ReadRecord(rowset.SelectRecord(source, rowset.Scope(source))).Count(ctx, session)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("nonterminating recursive query did not honor cancellation", err)
	}
	stop := errors.New("stop recursive traversal")
	yielded := false
	err = db.Session(t.Context(), func(session *database.Session) error {
		session.Discard()
		if _, err := session.Exec(t.Context(), `SET search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		anchor := recursivequeries.Nodes.Query().Where(orm.TextEq(recursivequeries.Nodes.Code, "root"))
		loop := rowset.RecursiveAllCTE("stream_loop", rowset.ModelRecord(anchor), func(self rowset.RecursiveSelf[recursivequeries.Node]) rowset.RecordSource[recursivequeries.Node] {
			return self
		})
		source := rowset.AsRecord[resultAlias](loop, "result")
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		return rowset.EachRead(ctx, session, rowset.SelectRecord(source, rowset.Scope(source)), func(row recursivequeries.Node) error {
			yielded = row.Code == "root"
			cancel() // Stop server work before Close can drain the endless stream.
			return stop
		})
	})
	if !yielded || !errors.Is(err, stop) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("recursive stream did not cancel after delivering its first row", err)
	}
	if _, err := db.Exec(t.Context(), "SELECT 1"); err != nil {
		t.Fatal("pool unusable after recursive cancellation", err)
	}
}

func TestRecursiveSelfCannotEscapeToExecutor(t *testing.T) {
	var escaped rowset.RecursiveSelf[models.User]
	_ = rowset.RecursiveCTE("tree", rowset.ModelRecord(models.Users.Query()), func(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
		escaped = self
		return descendants(self)
	})
	a := rowset.AsRecord[resultAlias](escaped, "escaped")
	if rows, err := rowset.ReadRecord(rowset.SelectRecord(a, rowset.Scope(a))).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("escaped self reached executor", err)
	}
}
