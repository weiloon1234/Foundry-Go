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
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type parentAlias struct{}
type childAlias struct{}
type resultAlias struct{}

func descendants(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
	parent := query.As[parentAlias](self, "parent")
	child := query.As[childAlias](models.QueryUsers(), "child")
	p, c := models.UserFieldsAt(parent.Scope()), models.UserFieldsAt(child.Scope())
	joined := query.InnerJoin(child, parent, query.On(c.IntroducerID, p.ID))
	return query.SelectRecord(joined, query.LeftScope(joined, child.Scope()))
}

func readUsers(t *testing.T, tx database.Executor, definition query.CommonTable[models.User]) []models.User {
	t.Helper()
	source := query.As[resultAlias](definition, "result")
	fields := models.UserFieldsAt(source.Scope())
	rows, err := query.SelectRecord(source, source.Scope()).OrderBy(fields.Email.Asc()).All(t.Context(), tx)
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
		users[2], err = models.QueryUsers().Update(ctx, tx, users[2].ID, models.UserDraft{}.SetIntroducerID(users[1].ID))
		if err != nil {
			return err
		}
		u := models.UserFields()
		anchor := models.QueryUsers().Where(u.ID.Eq(users[0].ID))
		tree := query.RecursiveCTE("descendants", anchor, descendants)
		if rows := readUsers(t, tx, tree); !reflect.DeepEqual(rows, users) {
			t.Fatal("recursive model hierarchy lost complete records or levels")
		}
		if rows := readUsers(t, tx, tree.Materialized()); !reflect.DeepEqual(rows, users) {
			t.Fatal("materialization changed recursion")
		}
		if rows := readUsers(t, tx, query.RecursiveCTE("empty_tree", anchor.Where(u.Age.Lt(0)), descendants)); len(rows) != 0 {
			t.Fatal("empty recursive anchor produced rows")
		}
		pruned := query.RecursiveCTE("pruned", anchor, func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			parent := query.As[parentAlias](self, "parent")
			child := query.As[childAlias](models.QueryUsers().Where(u.Age.Lt(22)), "child")
			j := query.InnerJoin(child, parent, query.On(models.UserFieldsAt(child.Scope()).IntroducerID, models.UserFieldsAt(parent.Scope()).ID))
			return query.SelectRecord(j, query.LeftScope(j, child.Scope()))
		})
		if rows := readUsers(t, tx, pruned); !reflect.DeepEqual(rows, users[:2]) {
			t.Fatal("recursive step predicate did not prune descendants")
		}
		roots := models.QueryUsers().Where(u.Age.Lte(21))
		if rows := readUsers(t, tx, query.RecursiveCTE("distinct_paths", roots, descendants)); !reflect.DeepEqual(rows, users) {
			t.Fatal("UNION did not deduplicate recursive paths")
		}
		if rows := readUsers(t, tx, query.RecursiveAllCTE("all_paths", roots, descendants)); !reflect.DeepEqual(rows, []models.User{users[0], users[1], users[1], users[2], users[2]}) {
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
		users[0], err = models.QueryUsers().Update(ctx, tx, users[0].ID, models.UserDraft{}.SetIntroducerID(users[2].ID))
		if err != nil {
			return err
		}
		if rows := readUsers(t, tx, tree); !reflect.DeepEqual(rows, users) {
			t.Fatal("recursive UNION did not terminate a repeated-record cycle")
		}
		return nil
	})
}

func checkPermittedStepSources(t *testing.T, tx *database.Tx, anchor models.UserQuery) error {
	for name, step := range map[string]func(query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User]{
		"intersection": func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			return query.Intersect(self, models.QueryUsers())
		},
		"left_except": func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			return query.Except(self, models.QueryUsers().Limit(0))
		},
		"union_step": func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			return query.UnionAll(self, models.QueryUsers().Limit(0))
		},
		"preserved_left": func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			a, b := query.As[parentAlias](self, "a"), query.As[childAlias](models.QueryUsers().Limit(0), "b")
			j := query.LeftJoin(a, b, query.On(models.UserFieldsAt(a.Scope()).ID, models.UserFieldsAt(b.Scope()).ID))
			return query.SelectRecord(j, query.LeftScope(j, a.Scope()))
		},
		"preserved_right": func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			a, b := query.As[parentAlias](models.QueryUsers().Limit(0), "a"), query.As[childAlias](self, "b")
			j := query.RightJoin(a, b, query.On(models.UserFieldsAt(a.Scope()).ID, models.UserFieldsAt(b.Scope()).ID))
			return query.SelectRecord(j, query.RightScope(j, b.Scope()))
		},
		"independent_aggregate": func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			a := query.As[parentAlias](self, "a")
			count := query.SelectValue(models.QueryOrders(), query.Count[models.Order]().Value())
			return query.SelectRecord(a, a.Scope()).Where(query.ExistsQuery(a, count))
		},
	} {
		tree := query.RecursiveCTE(name, anchor, step)
		if rows := readUsers(t, tx, tree); len(rows) != 1 {
			t.Fatal("permitted recursive step changed repeated anchor", name)
		}
	}
	return nil
}

func checkRecursiveComposition(t *testing.T, tx *database.Tx, tree query.CommonTable[models.User], users []models.User) error {
	ctx := t.Context()
	combined := query.Union(tree, tree)
	fields := models.UserFieldsAt(combined.Scope())
	if n, err := combined.Count(ctx, tx); err != nil || n != 3 {
		t.Fatal("shared recursive CTE/set composition failed", err)
	}
	s, err := combined.Compile()
	if err != nil {
		return err
	}
	if strings.Count(s.SQL(), `"descendants" ("id"`) != 1 {
		t.Fatal("shared recursive CTE was emitted more than once")
	}
	ids := query.SelectValue(combined, fields.ID.Value())
	if n, err := models.QueryUsers().WhereHas(models.UserRelations().Referrals.Where(models.UserFields().ID.InQuery(ids))).Count(ctx, tx); err != nil || n != 2 {
		t.Fatal("relationship predicate lost recursive CTE dependency", err)
	}
	if n, err := models.QueryUsers().Where(models.UserFields().ID.InQuery(ids)).Count(ctx, tx); err != nil || n != 3 {
		t.Fatal("recursive membership failed", err)
	}
	result := query.As[resultAlias](tree, "result")
	f := models.UserFieldsAt(result.Scope())
	q := query.SelectRecord(result, result.Scope()).OrderBy(f.Email.Asc())
	if row, err := q.Offset(1).RequireFirst(ctx, tx); err != nil || row.ID != users[1].ID {
		t.Fatal("recursive required result lost selected window", err)
	}
	if row, err := q.Limit(0).First(ctx, tx); err != nil || row.IsSet() {
		t.Fatal("recursive optional result ignored zero limit", err)
	}
	stop := errors.New("stop recursive stream")
	if err := q.Each(ctx, tx, func(models.User) error { return stop }); !errors.Is(err, stop) {
		t.Fatal("recursive stream lost callback failure", err)
	}
	if n, err := q.Count(ctx, tx); err != nil || n != 3 {
		t.Fatal("recursive stream failed to release rows", err)
	}
	return nil
}

func checkRecursiveProjection(t *testing.T, tx *database.Tx, anchor models.UserQuery, users []models.User) error {
	u := models.UserFields()
	initial := reports.SelectUserSummary(anchor, reports.UserSummarySelection[models.User]{ID: u.ID.Value(), Email: u.Email.Value(), Nickname: u.Nickname.Value(), Status: u.Status.Value()})
	tree := query.RecursiveCTE("summaries", initial, func(self query.RecursiveSelf[reports.UserSummary]) query.RecordQuerySource[reports.UserSummary] {
		parent := query.As[parentAlias](self, "parent")
		child := query.As[childAlias](models.QueryUsers(), "child")
		j := query.InnerJoin(child, parent, query.On(models.UserFieldsAt(child.Scope()).IntroducerID, reports.UserSummaryFieldsAt(parent.Scope()).ID))
		f := models.UserFieldsAt(query.LeftScope(j, child.Scope()))
		return reports.ProjectUserSummary(j).SelectID(f.ID.Value()).SelectEmail(f.Email.Value()).SelectNickname(f.Nickname.Value()).SelectStatus(f.Status.Value()).Query()
	})
	a := query.As[resultAlias](tree, "result")
	f := reports.UserSummaryFieldsAt(a.Scope())
	rows, err := query.SelectRecord(a, a.Scope()).OrderBy(f.Email.Asc()).All(t.Context(), tx)
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
		for _, draft := range []recursivequeries.NodeDraft{
			recursivequeries.NodeDraft{}.SetCode("root").SetName("Root"),
			recursivequeries.NodeDraft{}.SetCode("child").SetParent("root").SetName("Child"),
			recursivequeries.NodeDraft{}.SetCode("leaf").SetParent("child").SetName("Leaf"),
		} {
			if _, err := recursivequeries.QueryNodes().Create(t.Context(), session, draft); err != nil {
				return err
			}
		}
		n := recursivequeries.NodeFields()
		anchor := recursivequeries.QueryNodes().Where(n.Code.Eq("root"))
		tree := query.RecursiveCTE("node_tree", anchor, func(self query.RecursiveSelf[recursivequeries.Node]) query.RecordQuerySource[recursivequeries.Node] {
			parent := query.As[parentAlias](self, "parent")
			// Derived self-reference remains a FROM source, not an expression subquery.
			working := query.As[resultAlias](query.SelectRecord(parent, parent.Scope()), "working")
			child := query.As[childAlias](recursivequeries.QueryNodes(), "child")
			j := query.InnerJoin(child, working, query.On(recursivequeries.NodeFieldsAt(child.Scope()).Parent, recursivequeries.NodeFieldsAt(working.Scope()).Code))
			return query.SelectRecord(j, query.LeftScope(j, child.Scope()))
		})
		a := query.As[resultAlias](tree, "result")
		fields := recursivequeries.NodeFieldsAt(a.Scope())
		codes, err := query.SelectValue(a, fields.Code.Value()).OrderBy(fields.Code.Asc()).All(t.Context(), session)
		if err != nil || !reflect.DeepEqual(codes, []recursivequeries.NodeCode{"child", "leaf", "root"}) {
			t.Fatal("recursive natural-key hierarchy failed", codes, err)
		}
		loop := query.RecursiveAllCTE("nonterminating", anchor, func(self query.RecursiveSelf[recursivequeries.Node]) query.RecordQuerySource[recursivequeries.Node] {
			return self
		})
		source := query.As[resultAlias](loop, "result")
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		_, err = query.SelectRecord(source, source.Scope()).Count(ctx, session)
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
		anchor := recursivequeries.QueryNodes().Where(recursivequeries.NodeFields().Code.Eq("root"))
		loop := query.RecursiveAllCTE("stream_loop", anchor, func(self query.RecursiveSelf[recursivequeries.Node]) query.RecordQuerySource[recursivequeries.Node] {
			return self
		})
		source := query.As[resultAlias](loop, "result")
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		return query.SelectRecord(source, source.Scope()).Each(ctx, session, func(row recursivequeries.Node) error {
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
	var escaped query.RecursiveSelf[models.User]
	_ = query.RecursiveCTE("tree", models.QueryUsers(), func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
		escaped = self
		return descendants(self)
	})
	a := query.As[resultAlias](escaped, "escaped")
	if rows, err := query.SelectRecord(a, a.Scope()).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("escaped self reached executor", err)
	}
}
