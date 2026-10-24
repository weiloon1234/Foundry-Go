package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"runtime"
	"strings"
	"testing"
)

func TestTerminalWritesPreflight(t *testing.T) {
	f := eachFields(Hooks[eachRow]{})
	writer := &noEachWriter{}
	if got, err := CreateEach(t.Context(), writer, f.Query(), nil); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	for _, q := range []Query[eachRow]{f.Query().Limit(1), f.Query().OrderBy(f.Key().Asc()), WithoutModelHooks(f.Query())} {
		if _, err := FirstOrCreate(t.Context(), writer, q); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := PruneBatch(t.Context(), writer, q, 1, false); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := CreateEach(t.Context(), writer, f.Query(), make([][]Assignment[eachRow], MaxPerModelWriteRows+1)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	foreign := eachFields(Hooks[eachRow]{})
	if _, err := CreateEach(t.Context(), writer, f.Query(), [][]Assignment[eachRow]{{foreign.Name.Set("wrong")}}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	soft := timedRecords()
	if _, err := PruneBatch(t.Context(), writer, soft.Query(), 1, true); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := ForcePruneBatch(t.Context(), writer, soft.Query(), SoftDeletable[timed]{}, 1, true); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatal(writer.calls)
	}
}
func TestCreateEachRollbackAndDraftIsolation(t *testing.T) {
	for _, exit := range []bool{false, true} {
		calls, commits := 0, 0
		failure := errors.New("stop")
		f := eachFields(Hooks[eachRow]{Creating: func(context.Context, *database.Tx, *Write[eachRow]) error {
			calls++
			if calls == 2 {
				if exit {
					runtime.Goexit()
				}
				return failure
			}
			return nil
		}, AfterCommit: func(context.Context, lifecycle.Operation, Changes[eachRow]) error { commits++; return nil }})
		var statements []string
		db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "stored"}}})
		got, err := CreateEach(t.Context(), db, f.Query(), [][]Assignment[eachRow]{{f.Key().Set(1), f.Name.Set("one")}, {f.Key().Set(2), f.Name.Set("two")}})
		if err == nil || len(got) != 0 || calls != 2 || commits != 0 {
			t.Fatal(got, err, calls, commits)
		}
	}
}
func TestLookupDefaultsAndStoredMatch(t *testing.T) {
	f := eachFields(Hooks[eachRow]{})
	for _, matches := range []bool{true, false} {
		var bindings [][]any
		db := testDB(t, testConnector{bindings: &bindings, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
			if strings.Contains(sql, "SELECT EXISTS") {
				return []string{"exists"}, [][]driver.Value{{matches}}, nil
			}
			if strings.HasPrefix(sql, "SELECT") {
				return []string{"id", "name"}, nil, nil
			}
			return []string{"id", "name"}, [][]driver.Value{{int64(1), "Ada"}}, nil
		}})
		got, err := FirstOrCreate(t.Context(), db, f.Query().Where(f.Name.Eq("Ada")), f.Key().Set(1))
		if matches {
			if err != nil || got.Name != "Ada" {
				t.Fatal(got, err)
			}
		} else if !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		found := false
		for _, b := range bindings {
			for _, v := range b {
				if v == "Ada" {
					found = true
				}
			}
		}
		if !found {
			t.Fatal(bindings)
		}
	}
}
func TestLookupExistingSkipsUnchosenDraftAndRetrieval(t *testing.T) {
	calls := 0
	f := Define("lookup_existing", func(b Builder[eachRow], m *eachRow) eachTable {
		return eachTable{TableOf(b, &m.ID), TextOf(b, &m.Name)}
	}, WithRetrievalHooks(func() RetrievalHooks[eachRow] { calls++; return RetrievalHooks[eachRow]{} }))
	db := testDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "Ada"}}})
	got, err := FirstOrCreate(t.Context(), db, f.Query(), Assignment[eachRow]{err: errors.New("unused")})
	if err != nil || got.Name != "Ada" || calls != 0 {
		t.Fatal(got, err, calls)
	}
}
func TestExplainBoundsRedactionAndNoRetrieval(t *testing.T) {
	calls := 0
	f := Define("explain_records", func(b Builder[eachRow], m *eachRow) eachTable {
		return eachTable{TableOf(b, &m.ID), TextOf(b, &m.Name)}
	}, WithRetrievalHooks(func() RetrievalHooks[eachRow] { calls++; return RetrievalHooks[eachRow]{} }))
	defer func() {
		if calls != 0 {
			t.Fatal("explain dispatched retrieval")
		}
	}()
	for _, document := range []string{`[{"Plan":{"Node Type":"Seq Scan","Filter":"secret-value","Startup Cost":0,"Total Cost":2,"Plan Rows":1,"Plan Width":8}}]`, strings.Repeat("x", (4<<20)+1), `[]`} {
		db := testDB(t, testConnector{columns: []string{"QUERY PLAN"}, values: [][]driver.Value{{document}}})
		plan, err := Explain(t.Context(), db, f.Query().Where(f.Name.Eq("secret-value")))
		if strings.HasPrefix(document, "[{") {
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", plan, plan, plan), "secret-value") {
				t.Fatal("plan values leaked")
			}
			data := plan.JSON()
			data[0] = 'x'
			if plan.JSON()[0] != '[' {
				t.Fatal("shared plan bytes")
			}
		} else if !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
