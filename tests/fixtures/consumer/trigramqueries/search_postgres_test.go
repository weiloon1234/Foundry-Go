package trigramqueries_test

import (
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"

	"foundry.test/consumer/trigramqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func sessionState(t *testing.T, ex database.Executor) (int64, string) {
	t.Helper()
	result, err := ex.Query(t.Context(), `SELECT pg_backend_pid(), current_setting('pg_trgm.similarity_threshold')`)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	var pid int64
	var threshold string
	if !result.Next() {
		t.Fatal("missing session setting", result.Err())
	}
	if err := result.Scan(&pid, &threshold); err != nil {
		t.Fatal(err)
	}
	return pid, threshold
}

func TestPostgresTrigramSearch(t *testing.T) {
	db := pgtest.Open(t, func(c *postgres.Config) { c.Pool.MaxOpen, c.Pool.MaxIdle = 1, 1 })
	namespace := pgtest.Namespace(t, db)
	f := trigramqueries.Declare(namespace)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		// All runs serialize extension installation on the same database-wide key.
		if _, err := tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended('foundry.test.pg_trgm.public', 0))`); err != nil {
			return err
		}
		_, err := tx.Exec(t.Context(), `CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Load pg_trgm on this backend before observing its baseline GUC.
	if _, err := db.Exec(t.Context(), `SELECT public.similarity('word', 'words')`); err != nil {
		t.Fatal(err)
	}
	pid, baseline := sessionState(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `", pg_catalog`,
			`CREATE TABLE entities (id bigint PRIMARY KEY, country text NOT NULL)`,
			`CREATE TABLE entity_names (id bigint PRIMARY KEY, entity_id bigint NOT NULL REFERENCES entities(id), normalized text NOT NULL)`,
			`CREATE TABLE addresses (id bigint PRIMARY KEY, entity_id bigint NOT NULL REFERENCES entities(id), latitude numeric NOT NULL, longitude numeric NOT NULL)`,
			`CREATE TABLE reviews (id bigint PRIMARY KEY, body text NOT NULL, optional_body text)`,
			`CREATE INDEX review_body_trgm ON reviews USING gin (body public.gin_trgm_ops)`,
			`CREATE INDEX name_normalized_trgm ON entity_names USING gin (normalized public.gin_trgm_ops)`,
			`INSERT INTO entities VALUES (1,'MY'),(2,'MY'),(3,'SG'),(4,'MY')`,
			`INSERT INTO entity_names VALUES (10,1,'words'),(11,1,'words'),(12,2,'word'),(13,3,'words'),(14,4,'words')`,
			`INSERT INTO addresses VALUES (20,1,1,2),(21,1,3,4),(22,2,5,6),(23,3,7,8),(24,4,9,10)`,
			`INSERT INTO reviews VALUES (1,'words','words'),(2,'word','word'),(3,'zebra',NULL),(4,'words','words')`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		for _, tc := range []struct {
			threshold float64
			want      []string
		}{
			{0.3, []string{"words", "word"}}, {0.6, []string{"words"}},
		} {
			if err := expr.SetTrigramThreshold(t.Context(), tx, tc.threshold); err != nil {
				return err
			}
			got, err := trigramqueries.SimilarReviews(t.Context(), tx, f.Reviews, "words", 4)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatal("threshold did not change matches", got, tc.want, err)
			}
		}
		duplicates, err := trigramqueries.FindDuplicates(t.Context(), tx, f, "words", "MY", 4)
		if err != nil {
			return err
		}
		want := []trigramqueries.Duplicate{
			{ID: 1, Normalized: "words", Similarity: 1, Latitude: decimal.FromInt64(1), Longitude: decimal.FromInt64(2)},
			{ID: 1, Normalized: "words", Similarity: 1, Latitude: decimal.FromInt64(3), Longitude: decimal.FromInt64(4)},
			{ID: 1, Normalized: "words", Similarity: 1, Latitude: decimal.FromInt64(1), Longitude: decimal.FromInt64(2)},
			{ID: 1, Normalized: "words", Similarity: 1, Latitude: decimal.FromInt64(3), Longitude: decimal.FromInt64(4)},
		}
		if !reflect.DeepEqual(duplicates, want) {
			t.Fatal("joined duplicate search", duplicates)
		}

		body := expr.Value(orm.TextExpr(f.Reviews.Body))
		score := expr.TrigramSimilarity(body, "words")
		source := rows.From(f.Reviews.Query().Where(orm.KeyNe(f.Reviews.Key(), 4)))
		ordered := rows.OrderScalar(rows.SelectScalar(source, expr.AsSelected(expr.Row(body))), expr.Desc(score), expr.Asc(expr.Value(orm.KeyExpr(f.Reviews.Key()))))
		got, err := rows.ReadScalar(ordered).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(got, []string{"words", "word", "zebra"}) {
			t.Fatal("similarity ordering", got, err)
		}
		if err := expr.SetTrigramThreshold(t.Context(), tx, 0.3); err != nil {
			return err
		}
		strict := rows.WhereScalar(rows.SelectScalar(source, expr.AsSelected(expr.Row(body))), expr.TrigramMatch(body, "words"), expr.Gte(score, 0.8))
		got, err = rows.ReadScalar(strict).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(got, []string{"words"}) {
			t.Fatal("per-query cutoff", got, err)
		}

		nullable := expr.TrigramSimilarityNullable(expr.Value(orm.NullableTextExpr(f.Reviews.OptionalBody)), "words")
		nullableQuery := rows.OrderScalar(rows.SelectScalar(source, expr.AsSelected(nullable)), expr.DescValue(expr.AsSelected(nullable)).NullsLast())
		nullableValues, err := rows.ReadScalar(nullableQuery).All(t.Context(), tx)
		if err != nil || len(nullableValues) != 3 {
			t.Fatal(nullableValues, err)
		}
		first, ok := nullableValues[0].Get()
		second, secondOK := nullableValues[1].Get()
		if !ok || !secondOK || first != 1 || math.Abs(second-4.0/7.0) > 1e-6 || !nullableValues[2].IsNull() {
			t.Fatal("NULL propagation or similarity hydration", nullableValues)
		}

		locked, err := rows.ForNoKeyUpdate(rows.ReadScalar(strict)).SkipLocked().All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(locked, []string{"words"}) {
			t.Fatal("trigram no-key-update lock", locked, err)
		}
		type left struct{}
		type right struct{}
		n, e := rows.As[left](f.Names.Query(), "n"), rows.As[right](f.Entities.Query(), "e")
		joined := rows.InnerJoin(n, e, rows.On(expr.At(rows.Scope(n), orm.NumberExpr(f.Names.EntityID)), expr.At(rows.Scope(e), orm.KeyExpr(f.Entities.Key()))))
		ns, es := rows.LeftScope(joined, rows.Scope(n)), rows.RightScope(joined, rows.Scope(e))
		names := rows.SelectRecord(joined, ns).Where(expr.TrigramMatch(expr.At(ns, orm.TextExpr(f.Names.Normalized)), trigramqueries.Normalized("words")))
		both := rows.LockRows(rows.ReadRecord(names), rows.NoKeyUpdateLock(rows.Target(ns)), rows.KeyShareLock(rows.Target(es)).NoWait())
		matches, err := both.All(t.Context(), tx)
		if err != nil || len(matches) != 5 {
			t.Fatal("joined lock clauses", len(matches), err)
		}

		if _, err := tx.Exec(t.Context(), `SET LOCAL enable_seqscan = off`); err != nil {
			return err
		}
		indexed := rows.WhereScalar(rows.SelectScalar(rows.From(f.Reviews.Query()), expr.AsSelected(expr.Row(body))), expr.TrigramMatch(body, "words"))
		plan, err := rows.Explain(t.Context(), tx, rows.ReadScalar(indexed))
		if err != nil {
			return err
		}
		foundIndex := false
		pending := []orm.PlanNode{plan.Root()}
		for len(pending) > 0 {
			node := pending[len(pending)-1]
			pending = append(pending[:len(pending)-1], node.Plans...)
			foundIndex = foundIndex || node.NodeType == "Bitmap Index Scan" && node.IndexName == "review_body_trgm"
		}
		if !foundIndex {
			t.Fatal("match did not use trigram GIN index", string(plan.JSON()))
		}
		// End on a value different from the original setting, so a leak cannot pass.
		changed := 0.6
		if baseline == "0.6" {
			changed = 0.8
		}
		return expr.SetTrigramThreshold(t.Context(), tx, changed)
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRestored := func(ex database.Executor) {
		t.Helper()
		nextPID, nextThreshold := sessionState(t, ex)
		if nextPID != pid || nextThreshold != baseline {
			t.Fatal("threshold leaked or backend was replaced", pid, nextPID, baseline, nextThreshold)
		}
	}
	assertRestored(db)
	t.Run("threshold range and savepoints", func(t *testing.T) {
		assertThreshold := func(tx *database.Tx, want float64) {
			t.Helper()
			actualPID, setting := sessionState(t, tx)
			got, err := strconv.ParseFloat(setting, 64)
			// current_setting displays real GUCs with limited significant digits.
			matches := got == want || want != 0 && math.Abs(got/want-1) < 1e-5
			if err != nil || actualPID != pid || !matches {
				t.Fatal("unexpected transaction threshold", setting, want, actualPID, err)
			}
		}
		err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			for _, threshold := range []float64{0x1p-1022, 1e-100, math.Copysign(0, -1), 0.3} {
				if err := expr.SetTrigramThreshold(t.Context(), tx, threshold); err != nil {
					return err
				}
				assertThreshold(tx, threshold)
			}
			for _, threshold := range []float64{math.SmallestNonzeroFloat64, 1e-310, math.Nextafter(0x1p-1022, 0)} {
				if err := expr.SetTrigramThreshold(t.Context(), tx, threshold); !errors.Is(err, fault.Invalid) {
					t.Fatal("subnormal threshold accepted", threshold, err)
				}
				// Rejection must leave the setting and the transaction usable.
				assertThreshold(tx, 0.3)
			}
			if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
				assertThreshold(child, 0.3)
				if err := expr.SetTrigramThreshold(t.Context(), child, 0.6); err != nil {
					return err
				}
				assertThreshold(child, 0.6)
				return nil
			}); err != nil {
				return err
			}
			assertThreshold(tx, 0.6)
			rollback := errors.New("intentional savepoint rollback")
			err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
				assertThreshold(child, 0.6)
				if err := expr.SetTrigramThreshold(t.Context(), child, 0.9); err != nil {
					return err
				}
				assertThreshold(child, 0.9)
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal("savepoint rollback lost its cause", err)
			}
			assertThreshold(tx, 0.6)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		assertRestored(db)
	})
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error { assertRestored(tx); return nil }); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("intentional transaction rollback")
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		changed := 0.9
		if baseline == "0.9" {
			changed = 0.7
		}
		if err := expr.SetTrigramThreshold(t.Context(), tx, changed); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	assertRestored(db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error { assertRestored(tx); return nil }); err != nil {
		t.Fatal(err)
	}
}
