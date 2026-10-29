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
	"github.com/weiloon1234/Foundry-Go/database/orm"
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
		base := models.Users.Query().OrderBy(orm.NumberAsc(models.Users.Age))
		for _, test := range []struct{ size, offset, limit int }{{1, 0, 3}, {2, 0, 3}, {3, 1, 1}, {2, 1, 10}, {1, 5, 2}, {2, 0, 0}} {
			e := &chunkExecutor{Executor: tx}
			var got []model.ID[models.User]
			var retained [][]models.User
			q := base.Offset(test.offset).Limit(test.limit)
			err := orm.Chunk(t.Context(), e, q, test.size, func(batch []models.User) error {
				if len(batch) == 0 || len(batch) > test.size {
					t.Fatal("unbounded/empty batch")
				}
				retained = append(retained, batch)
				for _, u := range batch {
					got = append(got, u.ID)
				}
				// The parent read must be closed before callback queries on this Tx.
				_, err := orm.Count(t.Context(), tx, models.Users.Query())
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
		limits := orm.DefaultRelationLimits()
		limits.MaxRows = 2
		eager := orm.WithRelationLimits(base.Limit(3).With(models.UserLinks.Orders), limits)
		e := &chunkExecutor{Executor: tx}
		seen := 0
		if err := orm.Chunk(t.Context(), e, eager, 1, func(batch []models.User) error {
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
		for i := 0; i < orm.DefaultChunkSize; i++ {
			if _, err := orm.Insert(t.Context(), tx, models.Users.Table, orm.TextSet(models.Users.Email, "chunk@example.test"), orm.NumberSet(models.Users.Age, 50+i), orm.EnumSet(models.Users.Status, models.StatusActive), orm.EnumSet(models.Users.Level, models.LevelBasic)); err != nil {
				return err
			}
		}
		e = &chunkExecutor{Executor: tx}
		seen = 0
		if err := orm.Each(t.Context(), e, base.With(models.UserLinks.Orders), func(u models.User) error {
			if !u.Orders.IsLoaded() {
				t.Fatal("Each did not eager load")
			}
			seen++
			_, err := orm.Count(t.Context(), tx, models.Users.Query())
			return err
		}); err != nil {
			return err
		}
		if seen != orm.DefaultChunkSize+3 || e.reads != 4 {
			t.Fatal("Each did not use bounded batches", seen, e.reads)
		}
		return nil
	})
}

func TestPostgresPrimaryKeyChunksOwnBoundariesDuringUpdates(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		u := models.Users
		expected, err := orm.All(t.Context(), tx, models.Users.Query().OrderBy(orm.KeyAsc(u.ID)))
		if err != nil {
			return err
		}
		q := models.Users.Query().Where(orm.EnumEq(u.Status, models.StatusActive))
		var visited []model.ID[models.User]
		if err := orm.ChunkByKey(t.Context(), tx, q, orm.KeyExpr(models.Users.ID), 1, func(batch []models.User) error {
			for i, user := range batch {
				visited = append(visited, user.ID)
				if _, err := orm.Update(t.Context(), tx, models.Users.Table.Query(), models.Users.Table.Key(), user.ID, orm.EnumSet(models.Users.Status, models.StatusDisabled)); err != nil {
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
			if _, err := orm.Insert(t.Context(), tx, models.Countries.Table, orm.KeySet(models.Countries.Code, code), orm.TextSet(models.Countries.Name, "country")); err != nil {
				return err
			}
		}
		var codes []models.CountryCode
		countries := models.Countries.Query().OrderBy(orm.KeyDesc(models.Countries.Code)).Limit(3)
		if err := orm.EachByKey(t.Context(), tx, countries, orm.KeyExpr(models.Countries.Code), 2, func(c models.Country) error { codes = append(codes, c.Code); return nil }); err != nil {
			return err
		}
		if !slices.Equal(codes, []models.CountryCode{"DD", "CC", "BB"}) {
			t.Fatal("descending natural-key traversal lost limit", codes)
		}
		var retained [][]models.Country
		if err := orm.ChunkByKey(t.Context(), tx, models.Countries.Query(), orm.KeyExpr(models.Countries.Code), 2, func(batch []models.Country) error { retained = append(retained, batch); return nil }); err != nil {
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
		q := models.Users.Query().OrderBy(orm.NumberAsc(models.Users.Age))
		stop := errors.New("stop chunk")
		e := &chunkExecutor{Executor: tx, failAt: 2, err: stop}
		seen := 0
		if err := orm.Chunk(t.Context(), e, q, 1, func([]models.User) error { seen++; return nil }); !errors.Is(err, stop) || seen != 1 {
			t.Fatal("later read failure lost earlier delivery", err)
		}
		e = &chunkExecutor{Executor: tx}
		seen = 0
		if err := orm.EachChunked(t.Context(), e, q, 2, func(models.User) error { seen++; return stop }); err != stop || seen != 1 || e.reads != 1 {
			t.Fatal("row callback error did not stop batch", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		seen = 0
		e = &chunkExecutor{Executor: tx}
		if err := orm.EachChunked(ctx, e, q, 2, func(models.User) error { seen++; cancel(); return nil }); !errors.Is(err, context.Canceled) || seen != 1 || e.reads != 1 {
			t.Fatal("cancellation did not stop within batch", err)
		}
		func() {
			defer func() {
				if recover() != stop {
					t.Fatal("chunk callback panic changed")
				}
			}()
			_ = orm.Chunk(t.Context(), tx, q, 1, func([]models.User) error { panic(stop) })
		}()
		if n, err := orm.Count(t.Context(), tx, q); err != nil || n != 3 {
			t.Fatal("callback panic held database rows", err)
		}
		seen = 0
		if err := orm.Chunk(t.Context(), tx, q.With(models.UserLinks.SingleOrder), 1, func([]models.User) error { seen++; return nil }); !errors.Is(err, database.TooManyRows) || seen != 0 {
			t.Fatal("failed eager batch partially published", err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'invalid' WHERE id = $1`, users[1].ID.String()); err != nil {
			return err
		}
		seen = 0
		if err := orm.Chunk(t.Context(), tx, q, 2, func([]models.User) error { seen++; return nil }); err == nil || seen != 0 {
			t.Fatal("failed model decode published partial batch")
		}
		return nil
	})
}

func TestChunkInvalidInputsUseNoDatabase(t *testing.T) {
	q := models.Users.Query()
	noop := func([]models.User) error { return nil }
	for _, run := range []func() error{
		func() error { return orm.Chunk(t.Context(), queryfixture.NoQueries(t), q, 0, noop) },
		func() error {
			return orm.ChunkByKey(t.Context(), queryfixture.NoQueries(t), q.Offset(1), orm.KeyExpr(models.Users.ID), 2, noop)
		},
		func() error {
			return orm.ChunkByKey(t.Context(), queryfixture.NoQueries(t), q.OrderBy(orm.NumberAsc(models.Users.Age)), orm.KeyExpr(models.Users.ID), 2, noop)
		},
		func() error {
			return orm.ChunkByKey(t.Context(), queryfixture.NoQueries(t), q, orm.KeyExpr(models.Users.ID), 2, nil)
		},
	} {
		if err := run(); err == nil {
			t.Fatal("invalid chunk accepted")
		}
	}
	var batches [][]models.User
	if err := orm.Chunk(t.Context(), queryfixture.NoQueries(t), q.Limit(0), 2, func(batch []models.User) error { batches = append(batches, batch); return nil }); err != nil || !reflect.DeepEqual(batches, [][]models.User(nil)) {
		t.Fatal("empty chunk executed", err)
	}
}
