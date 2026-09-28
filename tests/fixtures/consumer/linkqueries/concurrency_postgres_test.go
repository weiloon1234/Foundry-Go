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

type attachmentResult struct {
	pivot linkqueries.Membership
	err   error
}

func TestPostgresRelationEndpointLocksSurvivePivotHooks(t *testing.T) {
	db, _, namespace := linkDatabase(t)
	var member linkqueries.Member
	var group linkqueries.Group
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error {
		var err error
		member, group, err = endpoints(t, tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	entered, release := make(chan struct{}), make(chan struct{})
	releaseWriter := sync.OnceFunc(func() { close(release) })
	defer releaseWriter()
	trace := &linkqueries.Trace{OnPhase: func(ctx context.Context, phase string) error {
		if phase != "local.created" {
			return nil
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	done := make(chan attachmentResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		var result attachmentResult
		result.err = linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
			var err error
			result.pivot, err = linkqueries.MemberRelations().Groups.Attach(linkqueries.WithTrace(ctx, trace), tx, member, group, linkqueries.MembershipDraft{}.SetRole("admin"))
			return err
		})
		done <- result
	}()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("attachment ended before its hook: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for name, attempt := range map[string]func(*database.Tx) error{
		"source": func(tx *database.Tx) error {
			_, err := linkqueries.QueryLinkMembers().ForUpdate().NoWait().RequireFind(ctx, tx, member.ID)
			return err
		},
		"target": func(tx *database.Tx) error {
			_, err := linkqueries.QueryLinkGroups().ForUpdate().NoWait().RequireFind(ctx, tx, group.Code)
			return err
		},
	} {
		err := linkTransaction(ctx, db, namespace, attempt)
		var failure *database.Error
		if !errors.As(err, &failure) || failure.SQLState() != "55P03" {
			t.Fatalf("%s was not locked through the pivot hook: %v", name, err)
		}
	}
	releaseWriter()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		if _, err := linkqueries.QueryLinkMembers().ForUpdate().NoWait().RequireFind(ctx, tx, member.ID); err != nil {
			return err
		}
		_, err := linkqueries.QueryLinkGroups().ForUpdate().NoWait().RequireFind(ctx, tx, group.Code)
		return err
	}); err != nil {
		t.Fatal("relation completion retained endpoint locks", err)
	}
	if trace.Reads != 0 {
		t.Fatal("internal relation reads dispatched retrieval callbacks")
	}
}

func TestPostgresCompetingRelationAttachmentsRetainUniqueness(t *testing.T) {
	db, _, namespace := linkDatabase(t)
	var member linkqueries.Member
	var group linkqueries.Group
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error {
		var err error
		member, group, err = endpoints(t, tx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(t.Context(), `CREATE UNIQUE INDEX link_active_pair ON link_memberships(member_id,group_code) WHERE deleted_at IS NULL`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	inserted, release, competing := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseWriter := sync.OnceFunc(func() { close(release) })
	defer releaseWriter()
	firstTrace := &linkqueries.Trace{OnPhase: func(ctx context.Context, phase string) error {
		if phase != "local.created" {
			return nil
		}
		close(inserted)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	secondTrace := &linkqueries.Trace{OnPhase: func(_ context.Context, phase string) error {
		if phase == "local.creating" {
			close(competing)
		}
		return nil
	}}
	attach := func(trace *linkqueries.Trace, done chan<- attachmentResult) {
		defer workers.Done()
		var result attachmentResult
		result.err = linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
			var err error
			result.pivot, err = linkqueries.MemberRelations().Groups.Attach(linkqueries.WithTrace(ctx, trace), tx, member, group, linkqueries.MembershipDraft{}.SetRole("admin"))
			return err
		})
		done <- result
	}
	firstDone, secondDone := make(chan attachmentResult, 1), make(chan attachmentResult, 1)
	workers.Add(1)
	go attach(firstTrace, firstDone)
	select {
	case <-inserted:
	case result := <-firstDone:
		t.Fatalf("first attachment failed before coordination: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	workers.Add(1)
	go attach(secondTrace, secondDone)
	select {
	case <-competing:
	case result := <-secondDone:
		t.Fatalf("second attachment failed before coordination: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	releaseWriter()
	select {
	case result := <-firstDone:
		if result.err != nil {
			t.Fatal(result.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case result := <-secondDone:
		if !errors.Is(result.err, database.UniqueViolation) {
			t.Fatal("competing attachment bypassed uniqueness", result.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if len(firstTrace.Committed) != 2 || len(secondTrace.Committed) != 0 {
		t.Fatal("failed competitor retained after-commit callbacks")
	}
	if err := linkTransaction(ctx, db, namespace, func(tx *database.Tx) error {
		count, err := linkqueries.QueryLinkMemberships().Count(ctx, tx)
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("competing attachment created duplicate pivots")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
