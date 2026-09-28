package chunkqueries_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

type chunkExecutor struct {
	database.Executor
	reads, failAt int
	err           error
}

func (e *chunkExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	e.reads++
	if e.reads == e.failAt {
		return nil, e.err
	}
	return e.Executor.Query(ctx, sql, args...)
}

func TestPostgresChunkWindowsAndEagerBudgets(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		base := models.QueryUsers().OrderBy(models.UserFields().Age.Asc())
		for _, test := range []struct{ size, offset, limit int }{{1, 0, 3}, {2, 0, 3}, {3, 1, 1}, {2, 1, 10}, {1, 5, 2}, {2, 0, 0}} {
			e := &chunkExecutor{Executor: tx}
			var got []model.ID[models.User]
			var retained [][]models.User
			q := base.Offset(test.offset).Limit(test.limit)
			err := q.Chunk(t.Context(), e, test.size, func(batch []models.User) error {
				if len(batch) == 0 || len(batch) > test.size {
					t.Fatal("unbounded/empty batch")
				}
				retained = append(retained, batch)
				for _, u := range batch {
					got = append(got, u.ID)
				}
				// The parent read must be closed before callback queries on this Tx.
				_, err := models.QueryUsers().Count(t.Context(), tx)
				return err
			})
			if err != nil {
				return err
			}
			var want []model.ID[models.User]
			for i := test.offset; i < len(users) && i < test.offset+test.limit; i++ {
				want = append(want, users[i].ID)
			}
			if !slices.Equal(got, want) {
				t.Fatal("chunk lost source window", test)
			}
			if test.limit == 0 && e.reads != 0 {
				t.Fatal("empty chunk window queried")
			}
			if len(retained) > 1 && retained[0][0].ID != got[0] {
				t.Fatal("later chunk reused caller backing array")
			}
		}
		limits := query.DefaultRelationLimits()
		limits.MaxRows = 2
		eager := base.Limit(3).With(models.UserRelations().Orders).WithRelationLimits(limits)
		e := &chunkExecutor{Executor: tx}
		seen := 0
		if err := eager.Chunk(t.Context(), e, 1, func(batch []models.User) error {
			orders, loaded := batch[0].Orders.Get()
			if !loaded || len(orders) != []int{2, 1, 0}[seen] {
				t.Fatal("incorrect eager chunk")
			}
			seen++
			return nil
		}); err != nil {
			return err
		}
		if seen != 3 || e.reads != 6 {
			t.Fatal("eager reads were not bounded per batch", e.reads)
		}
		// Each automatically uses complete batches when relations are requested.
		for i := 0; i < query.DefaultChunkSize; i++ {
			if _, err := models.QueryUsers().Create(t.Context(), tx, models.UserDraft{}.SetEmail("chunk@example.test").SetAge(50+i).SetStatus(models.StatusActive).SetLevel(models.LevelBasic)); err != nil {
				return err
			}
		}
		e = &chunkExecutor{Executor: tx}
		seen = 0
		if err := base.With(models.UserRelations().Orders).Each(t.Context(), e, func(u models.User) error {
			if !u.Orders.IsLoaded() {
				t.Fatal("Each did not eager load")
			}
			seen++
			_, err := models.QueryUsers().Count(t.Context(), tx)
			return err
		}); err != nil {
			return err
		}
		if seen != query.DefaultChunkSize+3 || e.reads != 4 {
			t.Fatal("Each did not use bounded batches", seen, e.reads)
		}
		return nil
	})
}

func TestPostgresPrimaryKeyChunksOwnBoundariesDuringUpdates(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		u := models.UserFields()
		expected, err := models.QueryUsers().OrderBy(u.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		q := models.QueryUsers().Where(u.Status.Eq(models.StatusActive))
		var visited []model.ID[models.User]
		if err := q.ChunkByID(t.Context(), tx, 1, func(batch []models.User) error {
			for i, user := range batch {
				visited = append(visited, user.ID)
				if _, err := models.QueryUsers().Update(t.Context(), tx, user.ID, models.UserDraft{}.SetStatus(models.StatusDisabled)); err != nil {
					return err
				}
				batch[i].ID = model.ID[models.User]{}
			}
			return nil
		}); err != nil {
			return err
		}
		if len(visited) != len(expected) {
			t.Fatal("updating filter fields skipped later keys")
		}
		for i, id := range visited {
			if id != expected[i].ID {
				t.Fatal("callback redirected traversal")
			}
		}
		if _, err := tx.Exec(t.Context(), `CREATE TABLE countries (code text PRIMARY KEY,name text NOT NULL)`); err != nil {
			return err
		}
		for _, code := range []models.CountryCode{"AA", "BB", "CC", "DD"} {
			if _, err := models.QueryCountries().Create(t.Context(), tx, models.CountryDraft{}.SetCode(code).SetName("country")); err != nil {
				return err
			}
		}
		var codes []models.CountryCode
		countries := models.QueryCountries().OrderBy(models.CountryFields().Code.Desc()).Limit(3)
		if err := countries.EachByID(t.Context(), tx, 2, func(c models.Country) error { codes = append(codes, c.Code); return nil }); err != nil {
			return err
		}
		if !slices.Equal(codes, []models.CountryCode{"DD", "CC", "BB"}) {
			t.Fatal("descending natural-key traversal lost limit", codes)
		}
		var retained [][]models.Country
		if err := models.QueryCountries().ChunkByID(t.Context(), tx, 2, func(batch []models.Country) error { retained = append(retained, batch); return nil }); err != nil {
			return err
		}
		if len(retained) != 2 || retained[0][0].Code != "AA" || retained[1][0].Code != "CC" {
			t.Fatal("key chunks reused retained batches")
		}
		return nil
	})
}

func TestPostgresChunkFailureAndCallbackSemantics(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := models.QueryUsers().OrderBy(models.UserFields().Age.Asc())
		stop := errors.New("stop chunk")
		e := &chunkExecutor{Executor: tx, failAt: 2, err: stop}
		seen := 0
		if err := q.Chunk(t.Context(), e, 1, func([]models.User) error { seen++; return nil }); !errors.Is(err, stop) || seen != 1 {
			t.Fatal("later read failure lost earlier delivery", err)
		}
		e = &chunkExecutor{Executor: tx}
		seen = 0
		if err := q.EachChunked(t.Context(), e, 2, func(models.User) error { seen++; return stop }); err != stop || seen != 1 || e.reads != 1 {
			t.Fatal("row callback error did not stop batch", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		seen = 0
		e = &chunkExecutor{Executor: tx}
		if err := q.EachChunked(ctx, e, 2, func(models.User) error { seen++; cancel(); return nil }); !errors.Is(err, context.Canceled) || seen != 1 || e.reads != 1 {
			t.Fatal("cancellation did not stop within batch", err)
		}
		func() {
			defer func() {
				if recover() != stop {
					t.Fatal("chunk callback panic changed")
				}
			}()
			_ = q.Chunk(t.Context(), tx, 1, func([]models.User) error { panic(stop) })
		}()
		if n, err := q.Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("callback panic held database rows", err)
		}
		seen = 0
		if err := q.With(models.UserRelations().SingleOrder).Chunk(t.Context(), tx, 1, func([]models.User) error { seen++; return nil }); !errors.Is(err, database.TooManyRows) || seen != 0 {
			t.Fatal("failed eager batch partially published", err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'invalid' WHERE id = $1`, users[1].ID.String()); err != nil {
			return err
		}
		seen = 0
		if err := q.Chunk(t.Context(), tx, 2, func([]models.User) error { seen++; return nil }); err == nil || seen != 0 {
			t.Fatal("failed model decode published partial batch")
		}
		return nil
	})
}

func TestChunkInvalidInputsUseNoDatabase(t *testing.T) {
	q := models.QueryUsers()
	noop := func([]models.User) error { return nil }
	for _, run := range []func() error{
		func() error { return q.Chunk(t.Context(), queryfixture.NoQueries(t), 0, noop) },
		func() error { return q.Offset(1).ChunkByID(t.Context(), queryfixture.NoQueries(t), 2, noop) },
		func() error {
			return q.OrderBy(models.UserFields().Age.Asc()).ChunkByID(t.Context(), queryfixture.NoQueries(t), 2, noop)
		},
		func() error { return q.ChunkByID(t.Context(), queryfixture.NoQueries(t), 2, nil) },
	} {
		if err := run(); err == nil {
			t.Fatal("invalid chunk accepted")
		}
	}
	var batches [][]models.User
	if err := q.Limit(0).Chunk(t.Context(), queryfixture.NoQueries(t), 2, func(batch []models.User) error { batches = append(batches, batch); return nil }); err != nil || !reflect.DeepEqual(batches, [][]models.User(nil)) {
		t.Fatal("empty chunk executed", err)
	}
}
