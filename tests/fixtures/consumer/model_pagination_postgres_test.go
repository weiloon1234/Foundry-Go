package consumer_test

import (
	"errors"
	"math"
	"slices"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGeneratedModelPagination(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, statement := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE write_records (code bigint PRIMARY KEY, name text NOT NULL, enabled boolean NOT NULL DEFAULT true, note text, amount numeric NOT NULL DEFAULT 0)`,
		} {
			if _, err := tx.Exec(t.Context(), statement); err != nil {
				return err
			}
		}
		q := models.QueryWriteRecords()
		fields := models.WriteRecordFields()
		for i := 1; i <= 9; i++ {
			amount, err := decimal.Parse([]string{"9007199254740993.1234567890123456789", "9007199254740993.1234567890123456788", "-1"}[i%3])
			if err != nil {
				return err
			}
			draft := models.WriteRecordDraft{}.SetCode(models.RecordKey(i)).SetName([]string{"a", "a", "b"}[i%3]).SetAmount(amount).SetEnabled(i%2 == 0)
			if i%3 != 0 {
				draft = draft.SetNote([]string{"", "x", "x"}[i%3])
			}
			if _, err := q.Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		for _, number := range []int{1, 2, 3, 4, 99} {
			page, err := q.Paginate(t.Context(), tx, query.PageRequest{Number: number, Size: 3})
			if err != nil {
				return err
			}
			if page.Total != 9 || page.Pages != 3 || page.Number != number || page.Size != 3 {
				t.Error("numbered page metadata changed")
			}
			var expected []models.RecordKey
			if number <= 3 {
				for i := 0; i < 3; i++ {
					expected = append(expected, models.RecordKey((number-1)*3+i+1))
				}
			}
			if !slices.Equal(recordKeys(page.Items), expected) {
				t.Error("numbered page did not use stable key ordering")
			}
			simple, err := q.SimplePaginate(t.Context(), tx, query.PageRequest{Number: number, Size: 3})
			if err != nil {
				return err
			}
			if !slices.Equal(recordKeys(simple.Items), expected) || simple.HasMore != (number < 3) {
				t.Error("simple page lost natural-key ordering or lookahead")
			}
		}
		empty, err := q.Where(fields.Code.Eq(999)).Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 3})
		if err != nil {
			return err
		}
		if empty.Total != 0 || empty.Pages != 0 || len(empty.Items) != 0 {
			t.Error("empty page counts changed")
		}
		if _, err := q.Paginate(t.Context(), tx, query.PageRequest{Number: math.MaxInt, Size: 2}); !errors.Is(err, fault.Invalid) {
			t.Error("offset overflow reached the database")
		}
		for index, orders := range [][]query.Order[models.WriteRecord]{
			nil, {fields.Name.Asc()}, {fields.Note.Asc()}, {fields.Note.Desc()},
			{fields.Enabled.Desc(), fields.Note.Asc(), fields.Amount.Desc()},
			{fields.Amount.Asc(), fields.Code.Desc()},
		} {
			base := q.OrderBy(orders...)
			// Match the documented appended primary tie-breaker unless supplied.
			expectedQuery := base
			if index != 5 {
				expectedQuery = expectedQuery.OrderBy(fields.Code.Asc())
			}
			expected, err := expectedQuery.All(t.Context(), tx)
			if err != nil {
				return err
			}
			for _, size := range []int{1, 2, 4} {
				request := query.CursorRequest[models.WriteRecord]{Size: size}
				var collected []models.RecordKey
				var prior []models.RecordKey
				for step := 0; step < 12; step++ {
					page, err := base.CursorPaginate(t.Context(), tx, request)
					if err != nil {
						return err
					}
					if len(page.Items) > size || page.Size != size {
						t.Error("cursor size not bounded")
					}
					if previous, set := page.Previous.Get(); set {
						back, err := base.CursorPaginate(t.Context(), tx, query.CursorRequest[models.WriteRecord]{Size: size, Before: value.Set(previous)})
						if err != nil {
							return err
						}
						if !slices.Equal(recordKeys(back.Items), prior) {
							t.Errorf("backward page differs for ordering %d", index)
						}
					} else if step != 0 {
						t.Error("cursor lost previous navigation")
					}
					prior = recordKeys(page.Items)
					collected = append(collected, prior...)
					next, set := page.Next.Get()
					if !set {
						break
					}
					parsed, err := query.ParseCursor[models.WriteRecord](next.Token())
					if err != nil {
						return err
					}
					request.After = value.Set(parsed)
					if step == 11 {
						t.Error("cursor traversal did not terminate")
					}
				}
				if !slices.Equal(collected, recordKeys(expected)) {
					t.Errorf("cursor skipped/repeated rows for ordering %d size %d", index, size)
				}
			}
		}
		first, err := q.CursorPaginate(t.Context(), tx, query.CursorRequest[models.WriteRecord]{Size: 2})
		if err != nil {
			return err
		}
		next, ok := first.Next.Get()
		if !ok {
			return errors.New("first page has no next cursor")
		}
		if _, err := q.Where(fields.Enabled.Eq(true)).CursorPaginate(t.Context(), tx, query.CursorRequest[models.WriteRecord]{Size: 2, After: value.Set(next)}); !errors.Is(err, fault.Invalid) {
			t.Error("changed query accepted old cursor")
		}
		wrongModel, err := query.ParseCursor[models.Country](next.Token())
		if err != nil {
			return err
		}
		if _, err := models.QueryCountries().CursorPaginate(t.Context(), tx, query.CursorRequest[models.Country]{Size: 2, After: value.Set(wrongModel)}); !errors.Is(err, fault.Invalid) {
			t.Error("transport cursor crossed model boundary")
		}
		// Inserts before an existing keyset position must not shift its boundary.
		for _, key := range []models.RecordKey{0, 100} {
			if _, err := q.Create(t.Context(), tx, models.WriteRecordDraft{}.SetCode(key).SetName("later")); err != nil {
				return err
			}
		}
		rest, err := q.CursorPaginate(t.Context(), tx, query.CursorRequest[models.WriteRecord]{Size: 20, After: value.Set(next)})
		if err != nil {
			return err
		}
		if !slices.Equal(recordKeys(rest.Items), []models.RecordKey{3, 4, 5, 6, 7, 8, 9, 100}) || rest.Next.IsSet() {
			t.Error("insert shifted cursor boundary")
		}
		return nil
	}, database.TxOptions{Isolation: database.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
}

func recordKeys(records []models.WriteRecord) []models.RecordKey {
	keys := make([]models.RecordKey, len(records))
	for i, record := range records {
		keys[i] = record.Code
	}
	return keys
}
