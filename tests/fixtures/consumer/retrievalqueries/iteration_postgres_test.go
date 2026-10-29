package retrievalqueries_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"

	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func TestPostgresRetrievalIterationClosesBoundedBatches(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
		q := retrievalqueries.Members.Query()
		f := retrievalqueries.Members
		for i := range orm.DefaultChunkSize + 3 {
			if _, err := orm.Insert(t.Context(), tx, retrievalqueries.Members.Table, orm.InputTextSet(retrievalqueries.Members.Name, fmt.Sprintf("stream %03d", i))); err != nil {
				return err
			}
		}
		base := q.Where(orm.InputTextLike(f.Name, "stream %"))
		window := base.OrderBy(orm.InputTextAsc(f.Name)).Offset(1).Limit(orm.DefaultChunkSize + 1)

		for _, test := range []struct {
			name string
			run  func(context.Context, database.Executor, func(retrievalqueries.Member) error) error
		}{
			{"model", func() func(arg0 context.Context, arg1 database.Executor, arg2 func(retrievalqueries.Member) error) error {
				receiver := window
				return func(arg0 context.Context, arg1 database.Executor, arg2 func(retrievalqueries.Member) error) error {
					return orm.Each(arg0, arg1, receiver, arg2)
				}
			}()},
		} {
			for _, stopEarly := range []bool{false, true} {
				var trace retrievalqueries.Trace
				ctx := retrievalqueries.WithTrace(t.Context(), &trace)
				before := stats.Member[0].Load()
				visits := 0
				stop := errors.New("consumer stopped iteration")
				err := test.run(ctx, &readWrapper{Executor: tx}, func(member retrievalqueries.Member) error {
					visits++
					if visits == 1 && len(trace.Calls) != orm.DefaultChunkSize*3 {
						return errors.New("retrieval did not finish exactly one bounded batch before yielding")
					}
					if member.Name != fmt.Sprintf("stream %03d", visits) {
						return errors.New("iteration changed its stored order or selected window")
					}
					if _, err := orm.Count(ctx, tx, q); err != nil {
						return err
					}
					if stopEarly {
						return stop
					}
					return nil
				})
				wantVisits, wantHydrated, wantFactories := orm.DefaultChunkSize+1, orm.DefaultChunkSize+1, 2
				if stopEarly {
					wantVisits, wantHydrated, wantFactories = 1, orm.DefaultChunkSize, 1
					if !errors.Is(err, stop) {
						return fmt.Errorf("%s lost early stop: %w", test.name, err)
					}
				} else if err != nil {
					return err
				}
				if visits != wantVisits || len(trace.Calls) != wantHydrated*3 || trace.LocalFactories != wantFactories || stats.Member[0].Load()-before != int64(wantFactories) {
					return fmt.Errorf("%s lost batch boundaries: visits=%d callbacks=%d local factories=%d", test.name, visits, len(trace.Calls), trace.LocalFactories)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
