package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestReportEachStreamsAndOwnsClosure(t *testing.T) {
	stop := errors.New("stop")
	for _, kind := range []string{"stop", "panic", "goexit", "decode", "cancel", "close"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			state := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(9), "Ada"}, {"invalid integer", "never"}}}
			if kind == "close" {
				state.rows = state.rows[:1]
				state.closeErr = stop
			}
			db := openTest(t, state)
			calls := 0
			err := EachRead(ctx, db, Read(projection()), func(r record) error {
				calls++
				if r != (record{9, "Ada"}) {
					t.Fatal(r)
				}
				switch kind {
				case "stop":
					return stop
				case "panic":
					panic("secret")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					cancel()
				}
				return nil
			})
			if err == nil || calls != 1 || !state.closed {
				t.Fatalf("err=%v calls=%d closed=%v", err, calls, state.closed)
			}
			if kind == "stop" && !errors.Is(err, stop) {
				t.Fatal(err)
			}
			if (kind == "panic" || kind == "goexit") && !errors.Is(err, fault.Panicked) {
				t.Fatal(err)
			}
		})
	}
}

func TestReportPreflightRejectsBeforeIO(t *testing.T) {
	m := declareMembers()
	id := expr.Value(m.ID)
	base := SelectValue(m.Query(), m.ID)
	ordered := OrderBy(base, id.Asc())
	state := &driverState{}
	db := openTest(t, state)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := PreflightRead(ctx, Read(ordered)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, q := range []Query[member, int64]{ReorderUnwindowed(base), ReorderUnwindowed(Limit(base, 1), id.Asc()), ReorderUnwindowed(Offset(base, 1), id.Asc()), ReorderUnwindowed(DistinctOn(base, expr.Group(id)), id.Asc()), {}} {
		if _, err := CompileRead(Read(q)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := Read(q).Count(t.Context(), db); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	for _, q := range []Query[member, int64]{base, Limit(ordered, 1), Offset(ordered, 1), {}} {
		if _, err := Read(q).Paginate(t.Context(), db, orm.Page(1, 1)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := Read(q).SimplePaginate(t.Context(), db, orm.Page(1, 1)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if err := EachRead(t.Context(), db, Read(ordered), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if state.sql != "" {
		t.Fatal("invalid report performed I/O")
	}
	before, err := CompileRead(Read(ordered))
	if err != nil {
		t.Fatal(err)
	}
	after, err := CompileRead(Read(ReorderUnwindowed(ordered, id.Desc())))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := CompileRead(Read(ordered))
	if before.SQL() != again.SQL() || !strings.HasSuffix(after.SQL(), "DESC") {
		t.Fatal("reorder mutated source")
	}
}

func TestReportSimplePageAndCount(t *testing.T) {
	m := declareMembers()
	q := OrderBy(SelectValue(m.Query(), m.ID), expr.Value(m.ID).Asc())
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(1)}, {int64(2)}}}
	page, err := Read(q).SimplePaginate(t.Context(), openTest(t, state), orm.Page(1, 1))
	if err != nil || len(page.Items) != 1 || !page.HasMore || cap(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if !strings.HasSuffix(state.sql, "LIMIT $1") {
		t.Fatal(state.sql)
	}
	countState := &driverState{columns: []string{"count"}, rows: [][]driver.Value{{int64(2)}}}
	n, err := Read(Distinct(q)).Count(t.Context(), openTest(t, countState))
	if err != nil || n != 2 || !strings.HasPrefix(countState.sql, "SELECT COUNT(*) FROM (SELECT DISTINCT") || strings.Contains(countState.sql, "ORDER BY") {
		t.Fatal(n, err, countState.sql)
	}
}

func TestProjectionBridgeCapabilitiesFailClosed(t *testing.T) {
	m := declareMembers()
	if _, err := orm.FoundryStatement(querycore.Statement{}, sqlowner.MetadataSeal{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	p := orm.FoundryPredicate(m.ID.Eq(1), sqlowner.MetadataSeal{})
	if !errors.Is(p.Err, fault.Invalid) {
		t.Fatal(p.Err)
	}
	var relation orm.HasMany[member, order]
	blocked := orm.FoundryRelationPredicate(relation, nil, sqlowner.MetadataSeal{})
	if !errors.Is(orm.FoundryPredicate(blocked, sqlowner.Metadata()).Err, fault.Invalid) {
		t.Fatal("zero relation capability accepted")
	}
	filter := orm.FoundryRelationFilter(relation, ormcore.Predicate{}, sqlowner.MetadataSeal{})
	if !errors.Is(orm.FoundryPredicate(filter, sqlowner.Metadata()).Err, fault.Invalid) {
		t.Fatal("zero filter capability accepted")
	}

	own, foreign := declareMembers(), declareMembers()
	for _, p := range []expr.Predicate[member]{expr.ModelPredicate(foreign.ID.Eq(1)), expr.ModelPredicate(orm.Predicate[member]{})} {
		if _, err := CompileRead(Read(SelectValue(own.Query(), own.ID).Where(p))); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

type reportTenantKey struct{}

func TestReportScopeContextIsFreshAtPreflightAndExecution(t *testing.T) {
	m := orm.Define("members", func(b orm.Builder[member], m *member) members {
		table := orm.TableOf(b, &m.ID)
		return members{table, orm.PrimaryOf(b, table.Key()), orm.TextOf(b, &m.Name), orm.NullableTextOf(b, &m.Nickname)}
	}, orm.WithGlobalScopes(func(d members) []orm.GlobalScope[member] {
		return []orm.GlobalScope[member]{orm.Scope(d.Table, "tenant", func(ctx context.Context) (orm.Predicate[member], error) {
			id, ok := ctx.Value(reportTenantKey{}).(int64)
			if !ok {
				return orm.Predicate[member]{}, errors.New("missing tenant")
			}
			return d.ID.Eq(id), nil
		})}
	}))
	bound := context.WithValue(t.Context(), reportTenantKey{}, int64(1))
	current := context.WithValue(t.Context(), reportTenantKey{}, int64(2))
	q := SelectValue(orm.WithScopeContext(m.Query(), bound), m.ID)
	s, err := CompileRead(Read(q))
	if err != nil || s.Arguments()[0] != int64(1) {
		t.Fatal(s, err)
	}
	if err := PreflightRead(current, Read(q)); err != nil {
		t.Fatal(err)
	}
	if err := PreflightRead(t.Context(), Read(q)); err == nil {
		t.Fatal("execution inherited the stale bound context")
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(2)}}}
	if err := EachRead(current, openTest(t, state), Read(q), func(int64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(state.arguments) != 1 || state.arguments[0].Value != int64(2) {
		t.Fatal("stale execution scope", state.arguments)
	}
}
