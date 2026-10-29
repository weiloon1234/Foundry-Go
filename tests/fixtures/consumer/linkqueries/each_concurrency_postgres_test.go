package linkqueries_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestPostgresPerModelSelectionLocksAndConcurrentInsert(t *testing.T) {
	db, _, namespace := linkORMDatabase(t)
	q := linkqueries.Members
	fields := linkqueries.Members
	var original []linkqueries.Member
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error {
		if _, err := orm.CreateEach(t.Context(), tx, q.Query(), [][]orm.Assignment[linkqueries.Member]{
			[]orm.Assignment[linkqueries.Member]{orm.TextSet(linkqueries.Members.Name, "batch-one")},
			[]orm.Assignment[linkqueries.Member]{orm.TextSet(linkqueries.Members.Name, "batch-two")},
		}); err != nil {
			return err
		}
		var err error
		original, err = orm.All(t.Context(), tx, q.Query().OrderBy(orm.KeyAsc(fields.Key())))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(original) != 2 {
		t.Fatal("fixture did not create two members")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	entered, release := make(chan struct{}), make(chan struct{})
	releaseWriter := sync.OnceFunc(func() { close(release) })
	defer releaseWriter()
	type result struct {
		rows []linkqueries.Member
		err  error
	}
	done := make(chan result, 1)
	trace := &linkqueries.Trace{}
	calls := 0
	workers.Add(1)
	go func() {
		defer workers.Done()
		var output result
		output.err = linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
			var err error
			output.rows, err = orm.UpdateEach(linkqueries.WithTrace(ctx, trace), tx, q.Query().Where(orm.TextLike(fields.Name, "batch%")), 2, func(ctx context.Context, _ *database.Tx, current linkqueries.Member) ([]orm.Assignment[linkqueries.Member], error) {
				calls++
				if calls == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return []orm.Assignment[linkqueries.Member]{}, ctx.Err()
					}
				}
				return []orm.Assignment[linkqueries.Member]{orm.TextSet(linkqueries.Members.Name, current.Name+"-updated")}, nil
			})
			return err
		})
		done <- output
	}()
	select {
	case <-entered:
	case output := <-done:
		t.Fatalf("batch ended before coordination: %v", output.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(q.Query().Where(orm.KeyEq(q.Key(), original[1].ID))).NoWait())
		return err
	})
	var locked *database.Error
	if !errors.As(err, &locked) || locked.SQLState() != "55P03" {
		t.Fatal("later candidate was not locked before the first callback", err)
	}
	var late linkqueries.Member
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		var err error
		late, err = orm.Insert(ctx, tx, q.Table, orm.TextSet(linkqueries.Members.Name, "batch-late"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	releaseWriter()
	select {
	case output := <-done:
		if output.err != nil || len(output.rows) != 2 {
			t.Fatal("selected batch did not finish", output.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if calls != 2 || trace.Reads != 0 {
		t.Fatal("batch included a later insert or dispatched internal retrieval callbacks")
	}
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		stored, err := orm.RequireKey(ctx, tx, q.Table, late.ID)
		if err != nil {
			return err
		}
		if stored.Name != "batch-late" {
			return errors.New("batch claimed a row inserted after selection")
		}
		count, err := orm.Count(ctx, tx, q.Query().Where(orm.TextLike(fields.Name, "%-updated")))
		if err != nil {
			return err
		}
		if count != 2 {
			return errors.New("batch lost a selected update")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
