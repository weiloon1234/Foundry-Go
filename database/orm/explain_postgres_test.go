package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type recordingPlanExecutor struct {
	database.Executor
	sql       string
	arguments []any
}

func (e *recordingPlanExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	e.sql = sql
	e.arguments = append([]any(nil), args...)
	return e.Executor.Query(ctx, sql, args...)
}

func TestPostgresExplainDoesNotExecuteAndAnalyzeUsesActualTransaction(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, ddl := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE plan_input(id bigint PRIMARY KEY,rank bigint)`,
			`INSERT INTO plan_input(id,rank) VALUES(7,10),(8,20)`,
			`CREATE TABLE plan_hits(id bigint)`,
			`CREATE FUNCTION plan_touch(input bigint) RETURNS bigint LANGUAGE plpgsql VOLATILE AS $$ BEGIN INSERT INTO plan_hits(id) VALUES(input); RETURN input; END $$`,
			`CREATE VIEW records AS SELECT plan_touch(id) AS id,rank FROM plan_input`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		fields := explainTable(t)
		q := fields.Query().Where(KeyEq(fields.Key(), 7))
		statement, err := Compile(q)
		if err != nil {
			return err
		}
		recorder := &recordingPlanExecutor{Executor: tx}
		plan, err := Explain(t.Context(), recorder, q)
		if err != nil {
			return err
		}
		if plan.Analyzed() || plan.NodeCount() == 0 || plan.ExecutionMilliseconds().IsSet() || !strings.HasSuffix(recorder.sql, statement.SQL()) || !reflect.DeepEqual(recorder.arguments, statement.Arguments()) {
			return errors.New("plan changed statement, bindings or inspection mode")
		}
		var hits int64
		if err := database.ScanOne(t.Context(), tx, `SELECT count(*) FROM plan_hits`, nil, &hits); err != nil {
			return err
		}
		if hits != 0 {
			return errors.New("ordinary Explain executed the selected query")
		}
		rollback := errors.New("rollback analyzed work")
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			plan, err := ExplainAnalyze(t.Context(), child, q)
			if err != nil {
				return err
			}
			if !plan.Analyzed() || !plan.ExecutionMilliseconds().IsSet() {
				return errors.New("analysis omitted actual measurements")
			}
			if err := database.ScanOne(t.Context(), child, `SELECT count(*) FROM plan_hits`, nil, &hits); err != nil {
				return err
			}
			if hits == 0 {
				return errors.New("ExplainAnalyze did not execute selected query")
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return errors.New("analysis failed to return its rollback outcome")
		}
		if err := database.ScanOne(t.Context(), tx, `SELECT count(*) FROM plan_hits`, nil, &hits); err != nil {
			return err
		}
		if hits != 0 {
			return errors.New("analysis escaped its actual transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAnalyzeRetainsRowLocksAndContextCancellation(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, ddl := range []string{`SET LOCAL search_path TO "` + namespace + `"`, `CREATE TABLE records(id bigint PRIMARY KEY,rank bigint)`, `INSERT INTO records(id,rank)VALUES(1,10)`} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	withPath := func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+namespace+`"`)
		return err
	}
	err := db.Transaction(t.Context(), func(holder *database.Tx) error {
		if err := withPath(holder); err != nil {
			return err
		}
		q := Lock(explainTable(t).Query())
		if _, err := ExplainLocked(t.Context(), holder, q); err != nil {
			return err
		}
		if err := db.Transaction(t.Context(), func(other *database.Tx) error {
			if err := withPath(other); err != nil {
				return err
			}
			_, err := ExplainAnalyzeLocked(t.Context(), other, q.NoWait())
			return err
		}); err != nil {
			return errors.New("planning acquired a row lock")
		}
		if _, err := ExplainAnalyzeLocked(t.Context(), holder, q); err != nil {
			return err
		}
		if err := db.Transaction(t.Context(), func(other *database.Tx) error {
			if err := withPath(other); err != nil {
				return err
			}
			_, err := ExplainAnalyzeLocked(t.Context(), other, q.NoWait())
			return err
		}); err == nil {
			return errors.New("analysis released its transaction's row lock")
		}
		if err := db.Transaction(t.Context(), func(other *database.Tx) error {
			if err := withPath(other); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err := ExplainAnalyzeLocked(ctx, other, q)
			if !errors.Is(err, context.DeadlineExceeded) {
				return errors.New("waiting analysis lost deadline")
			}
			return err
		}); !errors.Is(err, context.DeadlineExceeded) {
			return errors.New("analysis failed to return its deadline outcome")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type explainRecord struct {
	ID   int64
	Rank value.Nullable[int64]
}

func explainTable(t *testing.T) Table[explainRecord, int64] {
	t.Helper()
	c := codec.New(func(v int64) (driver.Value, error) { return v, nil }, func(any) (int64, error) {
		t.Error("plan hydrated a model")
		return 0, errors.New("unexpected hydration")
	}).WithParameterType(codec.TypeInteger)
	return Define("records", func(b Builder[explainRecord], m *explainRecord) Table[explainRecord, int64] {
		NullableNumberOf(b, &m.Rank, Codec(c))
		return TableOf(b, &m.ID)
	}, WithRetrievalHooks(func() RetrievalHooks[explainRecord] {
		t.Error("plan dispatched retrieval")
		return RetrievalHooks[explainRecord]{}
	}))
}
