// Package jobtest supplies one adapter contract for memory and Redis authorities.
package jobtest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type Fixture struct {
	Advance func(time.Duration)
	Backend jobs.Backend
	Key     jobs.Key
	Expire  func(jobs.Reservation)
}
type Payload struct {
	Exact uint64 `json:"exact"`
	Text  string `json:"text"`
}

func Run(t *testing.T, makeFixture func(*testing.T) Fixture) {
	t.Helper()
	t.Run("manual-retry", func(t *testing.T) { manualRetry(t, makeFixture) })
	t.Run("workflows", func(t *testing.T) { workflows(t, makeFixture); failureBoundaries(t, makeFixture) })
	t.Run("workflow-callbacks", func(t *testing.T) { callbacks(t, makeFixture) })
	t.Run("workflow-limits", func(t *testing.T) { workflowLimits(t, makeFixture) })
	t.Run("operations", func(t *testing.T) { operations(t, makeFixture) })
	t.Run("ownership-and-identity", func(t *testing.T) {
		f := makeFixture(t)
		ctx := t.Context()
		definition := jobs.Define[Payload]("contract.work", 1, jobs.DefaultPolicy(f.Key.Queue()))
		pending, err := definition.Capture(ctx, Payload{Exact: ^uint64(0), Text: "你好 / + %"}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			inserted, err := f.Backend.JobEnqueue(ctx, f.Key, pending.Envelope())
			if err != nil || inserted != (i == 0) {
				t.Fatal(inserted, err)
			}
		}
		var won atomic.Int32
		var claim jobs.Reservation
		var group sync.WaitGroup
		for range 16 {
			group.Go(func() {
				owner, err := lease.NewOwner()
				if err != nil {
					t.Error(err)
					return
				}
				found, err := f.Backend.JobReserve(ctx, f.Key, owner, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if r, ok := found.Get(); ok {
					won.Add(1)
					claim = r
				}
			})
		}
		group.Wait()
		if won.Load() != 1 {
			t.Fatalf("owners=%d", won.Load())
		}
		if claim.Envelope.PayloadJSON() != pending.Envelope().PayloadJSON() {
			t.Fatal("payload precision or spelling changed")
		}
		for range 2 {
			if attempt, err := f.Backend.JobStart(ctx, f.Key, claim.Ownership); err != nil || attempt != 1 {
				t.Fatal(attempt, err)
			}
		}
		f.Expire(claim)
		owner, err := lease.NewOwner()
		if err != nil {
			t.Fatal(err)
		}
		next, err := f.Backend.JobReserve(ctx, f.Key, owner, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		second, ok := next.Get()
		if !ok || second.Attempts != 1 || second.Envelope.ID() != claim.Envelope.ID() {
			t.Fatal("expired execution was lost")
		}
		if status, err := f.Backend.JobRenew(ctx, f.Key, claim.Ownership, time.Minute); err != nil || status.Owned {
			t.Fatal(status, err)
		}
		if _, err := f.Backend.JobStart(ctx, f.Key, claim.Ownership); !errors.Is(err, jobs.ErrOwnershipLost) {
			t.Fatal(err)
		}
		if ok, err := f.Backend.JobFinish(ctx, f.Key, claim.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || ok {
			t.Fatal(ok, err)
		}
		if attempt, err := f.Backend.JobStart(ctx, f.Key, second.Ownership); err != nil || attempt != 2 {
			t.Fatal(attempt, err)
		}
		foreign := pending.Envelope().Target()
		foreign.Version++
		if ok, err := f.Backend.JobCancel(ctx, f.Key, foreign); err != nil || ok {
			t.Fatal("foreign version cancelled", err)
		}
		if ok, err := f.Backend.JobCancel(ctx, f.Key, pending.Envelope().Target()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if status, err := f.Backend.JobRenew(ctx, f.Key, second.Ownership, time.Minute); err != nil || !status.Owned || !status.CancellationRequested {
			t.Fatal(status, err)
		}
		if ok, err := f.Backend.JobFinish(ctx, f.Key, second.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		found, err := f.Backend.JobInspect(ctx, f.Key, pending.Envelope().ID())
		if err != nil {
			t.Fatal(err)
		}
		record, ok := found.Get()
		if !ok || record.State != jobs.Cancelled || record.Attempts != 2 {
			t.Fatal(record)
		}
	})
	t.Run("unique-and-conflict", func(t *testing.T) {
		f := makeFixture(t)
		ctx := t.Context()
		d := jobs.Define[Payload]("contract.unique", 1, jobs.DefaultPolicy(f.Key.Queue()))
		key, err := jobs.NewUniqueKey[Payload]("business-key")
		if err != nil {
			t.Fatal(err)
		}
		options := jobs.Options[Payload]{Unique: jobs.Unique[Payload]{Key: key, For: time.Minute}}
		first, err := d.Capture(ctx, Payload{Exact: 1}, options)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := f.Backend.JobEnqueue(ctx, f.Key, first.Envelope()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if ok, err := f.Backend.JobEnqueue(ctx, f.Key, first.Envelope()); err != nil || ok {
			t.Fatal(ok, err)
		}
		other, err := d.Capture(ctx, Payload{Exact: 1}, options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(ctx, f.Key, other.Envelope()); !errors.Is(err, jobs.ErrNotUnique) {
			t.Fatal(err)
		}
		options.ID = first.ID()
		conflict, err := d.Capture(ctx, Payload{Exact: 2}, options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(ctx, f.Key, conflict.Envelope()); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := f.Backend.JobInspect(cancelled, f.Key, first.Envelope().ID()); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
