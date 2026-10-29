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

func TestPostgresLookupConcurrentAbsenceRetainsUniqueConflict(t *testing.T) {
	db, _, namespace := linkORMDatabase(t)
	var member linkqueries.Member
	var group linkqueries.Group
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error {
		var err error
		member, group, err = ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(t.Context(), `CREATE UNIQUE INDEX lookup_membership_identity ON link_memberships(member_id,group_code) WHERE deleted_at IS NULL`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	type outcome struct {
		model linkqueries.Membership
		err   error
	}
	type participant struct {
		entered, release chan struct{}
		done             chan outcome
		trace            *linkqueries.Trace
		attempts         int
		unblock          func()
	}
	participants := make([]*participant, 2)
	q := linkqueries.Memberships
	fields := linkqueries.Memberships
	selected := q.Query().Where(orm.ColumnEq(fields.MemberID, member.ID), orm.TextEq(fields.GroupCode, group.Code))
	for i := range participants {
		p := &participant{entered: make(chan struct{}), release: make(chan struct{}), done: make(chan outcome, 1), trace: &linkqueries.Trace{}}
		p.unblock = sync.OnceFunc(func() { close(p.release) })
		defer p.unblock()
		participants[i] = p
		p.trace.OnPhase = func(ctx context.Context, phase string) error {
			if phase == "local.creating" {
				p.attempts++
				if p.attempts != 1 {
					return errors.New("lookup creation retried its hooks")
				}
				close(p.entered)
				select {
				case <-p.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			var output outcome
			output.err = linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
				var err error
				output.model, err = orm.FirstOrCreate(linkqueries.WithTrace(ctx, p.trace), tx, selected, orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code), orm.InputTextSet(linkqueries.Memberships.Role, "admin"))
				return err
			})
			p.done <- output
		}()
	}
	for _, p := range participants {
		select {
		case <-p.entered:
		case result := <-p.done:
			t.Fatalf("lookup ended before both missing branches: %v", result.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	participants[0].unblock()
	var winner outcome
	select {
	case winner = <-participants[0].done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if winner.err != nil {
		t.Fatal(winner.err)
	}
	participants[1].unblock()
	select {
	case loser := <-participants[1].done:
		if !errors.Is(loser.err, database.UniqueViolation) {
			t.Fatal("competing lookup swallowed its ordinary uniqueness failure", loser.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if participants[0].attempts != 1 || participants[1].attempts != 1 || len(participants[0].trace.Committed) != 2 || len(participants[1].trace.Committed) != 0 {
		t.Fatal("lookup retry or losing after-commit work changed concurrency semantics")
	}
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		rows, err := orm.All(ctx, tx, q.Query())
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != winner.model.ID {
			return errors.New("concurrent lookup did not retain exactly its committed winner")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLookupLocksExistingModelBeforeDraftCallback(t *testing.T) {
	db, _, namespace := linkORMDatabase(t)
	q := linkqueries.Members
	fields := linkqueries.Members
	var member linkqueries.Member
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error {
		var err error
		member, err = orm.Insert(t.Context(), tx, q.Table, orm.TextSet(linkqueries.Members.Name, "original"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	done := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		done <- linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
			_, err := orm.UpdateOrCreate(ctx, tx, q.Query().Where(orm.KeyEq(fields.Key(), member.ID)), []orm.Assignment[linkqueries.Member]{}, func(ctx context.Context, _ *database.Tx, current linkqueries.Member) ([]orm.Assignment[linkqueries.Member], error) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return []orm.Assignment[linkqueries.Member]{}, ctx.Err()
				}
				return []orm.Assignment[linkqueries.Member]{orm.TextSet(linkqueries.Members.Name, current.Name+"-updated")}, nil
			})
			return err
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("lookup ended before its callback: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(q.Query().Where(orm.KeyEq(q.Key(), member.ID))).NoWait())
		return err
	})
	var locked *database.Error
	if !errors.As(err, &locked) || locked.SQLState() != "55P03" {
		t.Fatal("lookup did not lock the existing row before its callback", err)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		stored, err := orm.RequireKey(ctx, tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if stored.Name != "original-updated" {
			return errors.New("locked lookup lost its update")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
