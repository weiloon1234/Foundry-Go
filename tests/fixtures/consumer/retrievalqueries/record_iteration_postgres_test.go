package retrievalqueries_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type selectedMember struct{}

func TestPostgresCompleteRecordIterationClosesBoundedBatches(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
		q := retrievalqueries.Members.Query()
		f := retrievalqueries.Members
		for i := range query.DefaultChunkSize + 3 {
			if _, err := retrievalqueries.Members.Insert(t.Context(), tx, f.Name.Set(fmt.Sprintf("stream %03d", i))); err != nil {
				return err
			}
		}
		base := q.Where(f.Name.Like("stream %"))
		alias := rows.AsRecord[selectedMember](rows.CTE("iteration_members", rows.ModelRecord(base)), "selected")
		name := expr.At(rows.Scope(alias), f.Name)
		record := rows.SelectRecord(alias, rows.Scope(alias)).OrderBy(name.Asc()).Offset(1).Limit(query.DefaultChunkSize + 1)
		for _, test := range []struct {
			name string
			run  func(context.Context, database.Executor, func(retrievalqueries.Member) error) error
		}{
			{"complete-cte-record", func(ctx context.Context, ex database.Executor, yield func(retrievalqueries.Member) error) error {
				return rows.EachRead(ctx, ex, record, yield)
			}},
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
