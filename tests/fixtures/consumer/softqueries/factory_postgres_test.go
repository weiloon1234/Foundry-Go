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
