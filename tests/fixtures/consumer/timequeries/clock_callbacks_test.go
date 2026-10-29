package timequeries_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/timequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresTimestampClockCallbackFailures(t *testing.T) {
	runTimed(t, func(tx *database.Tx, clock *countingClock) error {
		q, f := timequeries.Members, timequeries.Members
		member, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(timequeries.Members.Name, "original"))
		if err != nil {
			return err
		}
		clock.source.Advance(time.Hour)
		for _, action := range []int32{1, 2} {
			for _, bulk := range []bool{false, true} {
				before := clock.calls.Load()
				clock.nextAction.Store(action)
				if bulk {
					result, failure := orm.Upsert(t.Context(), tx, q.Query(), orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoUpdate(orm.ConflictIncoming(orm.InputTextExpr(f.Name))), orm.KeySet(timequeries.Members.Key(), member.ID), orm.InputTextSet(timequeries.Members.Name, "failed"))
					if result.IsSet() {
						return errors.New("failed clock returned an upsert result")
					}
					err = failure
				} else {
					result, failure := orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(timequeries.Members.Name, "failed"))
					if result != (timequeries.Member{}) {
						return errors.New("failed clock returned a model")
					}
					err = failure
				}
				if !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private clock fixture payload") || clock.calls.Load() != before+1 {
					return errors.New("clock panic or Goexit escaped isolation, leaked its payload, or was retried")
				}
				current, err := orm.RequireKey(t.Context(), tx, q.Table, member.ID)
				if err != nil {
					return err
				}
				if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt {
					return errors.New("failed clock changed stored state")
				}
			}
		}
		updated, err := orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(timequeries.Members.Name, "recovered"))
		if err != nil {
			return err
		}
		if updated.Name != "recovered" || !updated.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) {
			return errors.New("parent scope was unusable after clock failure")
		}
		return nil
	})
}
