package softqueries_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func TestPostgresFactoryUsesGeneratedDraftsHooksAndTransaction(t *testing.T) {
	trace := &softqueries.Trace{}
	runSoft(t, func(tx *database.Tx, clock *testkit.Clock) error {
		members, err := factory.New[softqueries.Member](func(_ context.Context, n factory.Sequence) (softqueries.MemberDraft, error) {
			return softqueries.MemberDraft{}.SetName(fmt.Sprintf(" member-%d ", n)), nil
		})
		if err != nil {
			return err
		}
		ctx := softqueries.WithTrace(t.Context(), trace)
		created, err := members.CreateMany(ctx, tx, 2)
		if err != nil {
			return err
		}
		if len(created) != 2 || created[0].ID.IsZero() || created[0].ID == created[1].ID || created[0].Name != "member-1" || created[1].Name != "member-2" || !created[0].CreatedAt.Equal(clock.Now().Truncate(time.Microsecond)) {
			return errors.New("factory lost generated UUID, input ordering, mutator or timestamp")
		}
		want := []string{"local.saving", "provider.saving", "local.creating", "provider.creating", "local.created", "provider.created", "local.saved", "provider.saved"}
		if !reflect.DeepEqual(trace.Steps, append(append([]string{}, want...), want...)) {
			return errors.New("factory skipped ordinary per-model observers")
		}
		if len(trace.Committed) != 0 {
			return errors.New("factory committed before its caller")
		}
		duplicate, err := members.WithStates(func(_ context.Context, d softqueries.MemberDraft) (softqueries.MemberDraft, error) {
			return d.SetName("duplicate"), nil
		})
		if err != nil {
			return err
		}
		if items, err := duplicate.CreateMany(ctx, tx, 2); !errors.Is(err, database.UniqueViolation) || len(items) != 0 {
			return errors.New("factory published a partially failed batch")
		}
		count, err := softqueries.QuerySoftMembers().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 2 {
			return errors.New("factory batch escaped savepoint rollback")
		}
		veto := &softqueries.Trace{VetoAt: "local.created"}
		if _, err := members.Create(softqueries.WithTrace(t.Context(), veto), tx); !errors.Is(err, softqueries.Veto) {
			return errors.New("factory ignored model veto")
		}
		if len(veto.Committed) != 0 {
			return errors.New("failed factory retained after-commit work")
		}
		return nil
	})
	// Each committed model contributes one local and one provider commit callback.
	if len(trace.Committed) != 4 {
		t.Fatal("factory retained rolled-back callbacks or lost successful callbacks", len(trace.Committed))
	}
}

// AfterCreating hooks share the insert's transaction: a failing hook rolls back
// the model it follows and, for CreateMany, every model of the batch.
func TestPostgresFactoryHookFailureLeavesNoRows(t *testing.T) {
	runSoft(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := factory.New[softqueries.Member](func(_ context.Context, n factory.Sequence) (softqueries.MemberDraft, error) {
			return softqueries.MemberDraft{}.SetName(fmt.Sprintf("hooked-%d", n)), nil
		})
		if err != nil {
			return err
		}
		failure := errors.New("hook failed")
		var seen []int64
		failAt := 0
		hooked, err := members.AfterCreating(func(ctx context.Context, writer database.Transactor, member softqueries.Member) (softqueries.Member, error) {
			// The hook runs in the transaction that inserted its model.
			scoped, ok := writer.(*database.Tx)
			if !ok {
				return member, errors.New("hook did not receive the insert transaction")
			}
			visible, err := softqueries.QuerySoftMembers().Count(ctx, scoped)
			if err != nil {
				return member, err
			}
			seen = append(seen, visible)
			if len(seen) == failAt {
				return member, failure
			}
			return member, nil
		})
		if err != nil {
			return err
		}
		failAt = 2
		if items, err := hooked.CreateMany(t.Context(), tx, 3); !errors.Is(err, failure) || len(items) != 0 || !reflect.DeepEqual(seen, []int64{3, 3}) {
			return fmt.Errorf("failing hook batch = %d, %v, saw %v", len(items), err, seen)
		}
		seen, failAt = nil, 1
		if _, err := hooked.Create(t.Context(), tx); !errors.Is(err, failure) || !reflect.DeepEqual(seen, []int64{1}) {
			return fmt.Errorf("failing hook create: %v, saw %v", err, seen)
		}
		count, err := softqueries.QuerySoftMembers().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("a failing hook left %d row(s)", count)
		}
		seen, failAt = nil, 0
		if created, err := hooked.Create(t.Context(), tx); err != nil || created.ID.IsZero() {
			return fmt.Errorf("successful hook create: %v", err)
		}
		return nil
	})
}
