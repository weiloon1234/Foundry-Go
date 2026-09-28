package retrievalqueries_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type selectedMember struct{}

func TestPostgresRetrievalIterationClosesBoundedBatches(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
		q := retrievalqueries.QueryRetrievalMembers()
		f := retrievalqueries.MemberFields()
		for i := range query.DefaultChunkSize + 3 {
			if _, err := q.Create(t.Context(), tx, retrievalqueries.MemberDraft{}.SetName(fmt.Sprintf("stream %03d", i))); err != nil {
				return err
			}
		}
		base := q.Where(f.Name.Like("stream %"))
		window := base.OrderBy(f.Name.Asc()).Offset(1).Limit(query.DefaultChunkSize + 1)
		alias := query.As[selectedMember](query.CTE("iteration_members", base), "selected")
		af := retrievalqueries.MemberFieldsAt(alias.Scope())
		record := query.SelectRecord(alias, alias.Scope()).OrderBy(af.Name.Asc()).Offset(1).Limit(query.DefaultChunkSize + 1)
		for _, test := range []struct {
			name string
			run  func(context.Context, database.Executor, func(retrievalqueries.Member) error) error
		}{
			{"model", window.Each},
			{"complete-cte-record", record.Each},
		} {
			for _, stopEarly := range []bool{false, true} {
				var trace retrievalqueries.Trace
				ctx := retrievalqueries.WithTrace(t.Context(), &trace)
				before := stats.Member[0].Load()
				visits := 0
				stop := errors.New("consumer stopped iteration")
				err := test.run(ctx, &readWrapper{Executor: tx}, func(member retrievalqueries.Member) error {
					visits++
					if visits == 1 && len(trace.Calls) != query.DefaultChunkSize*3 {
						return errors.New("retrieval did not finish exactly one bounded batch before yielding")
					}
					if member.Name != fmt.Sprintf("stream %03d", visits) {
						return errors.New("iteration changed its stored order or selected window")
					}
					if _, err := q.Count(ctx, tx); err != nil {
						return err
					}
					if stopEarly {
						return stop
					}
					return nil
				})
				wantVisits, wantHydrated, wantFactories := query.DefaultChunkSize+1, query.DefaultChunkSize+1, 2
				if stopEarly {
					wantVisits, wantHydrated, wantFactories = 1, query.DefaultChunkSize, 1
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
