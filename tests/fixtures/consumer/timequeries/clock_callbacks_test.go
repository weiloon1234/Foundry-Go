package timequeries_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/timequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresTimestampClockCallbackFailures(t *testing.T) {
	runTimed(t, func(tx *database.Tx, clock *countingClock) error {
		q, f := timequeries.QueryTimeMembers(), timequeries.MemberFields()
		member, err := q.Create(t.Context(), tx, timequeries.MemberDraft{}.SetName("original"))
		if err != nil {
			return err
		}
		clock.source.Advance(time.Hour)
		for _, action := range []int32{1, 2} {
			for _, bulk := range []bool{false, true} {
				before := clock.calls.Load()
				clock.nextAction.Store(action)
				if bulk {
					result, failure := q.Upsert(t.Context(), tx, timequeries.MemberDraft{}.SetID(member.ID).SetName("failed"), query.OnConflict(f.ID).DoUpdate(f.Name.Incoming()))
					if result.IsSet() {
						return errors.New("failed clock returned an upsert result")
					}
					err = failure
				} else {
					result, failure := q.Update(t.Context(), tx, member.ID, timequeries.MemberDraft{}.SetName("failed"))
					if result != (timequeries.Member{}) {
						return errors.New("failed clock returned a model")
					}
					err = failure
				}
				if !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private clock fixture payload") || clock.calls.Load() != before+1 {
					return errors.New("clock panic or Goexit escaped isolation, leaked its payload, or was retried")
				}
				current, err := q.RequireFind(t.Context(), tx, member.ID)
				if err != nil {
					return err
				}
				if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt {
					return errors.New("failed clock changed stored state")
				}
			}
		}
		updated, err := q.Update(t.Context(), tx, member.ID, timequeries.MemberDraft{}.SetName("recovered"))
		if err != nil {
			return err
		}
		if updated.Name != "recovered" || !updated.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) {
			return errors.New("parent scope was unusable after clock failure")
		}
		return nil
	})
}
