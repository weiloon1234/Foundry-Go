package linkqueries_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func TestPostgresPerModelSelectionLocksAndConcurrentInsert(t *testing.T) {
	db, _, namespace := linkDatabase(t)
	q := linkqueries.QueryLinkMembers()
	fields := linkqueries.MemberFields()
	var original []linkqueries.Member
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error {
		if _, err := q.CreateEach(t.Context(), tx, []linkqueries.MemberDraft{
			linkqueries.MemberDraft{}.SetName("batch-one"),
			linkqueries.MemberDraft{}.SetName("batch-two"),
		}); err != nil {
			return err
		}
		var err error
		original, err = q.OrderBy(fields.ID.Asc()).All(t.Context(), tx)
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
			output.rows, err = q.Where(fields.Name.Like("batch%")).UpdateEach(linkqueries.WithTrace(ctx, trace), tx, 2, func(ctx context.Context, _ *database.Tx, current linkqueries.Member) (linkqueries.MemberDraft, error) {
				calls++
				if calls == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return linkqueries.MemberDraft{}, ctx.Err()
					}
				}
				return linkqueries.MemberDraft{}.SetName(current.Name + "-updated"), nil
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
		_, err := q.ForUpdate().NoWait().RequireFind(ctx, tx, original[1].ID)
		return err
	})
	var locked *database.Error
	if !errors.As(err, &locked) || locked.SQLState() != "55P03" {
		t.Fatal("later candidate was not locked before the first callback", err)
	}
	var late linkqueries.Member
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		var err error
		late, err = q.Create(ctx, tx, linkqueries.MemberDraft{}.SetName("batch-late"))
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
		stored, err := q.RequireFind(ctx, tx, late.ID)
		if err != nil {
			return err
		}
		if stored.Name != "batch-late" {
			return errors.New("batch claimed a row inserted after selection")
		}
		count, err := q.Where(fields.Name.Like("%-updated")).Count(ctx, tx)
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
