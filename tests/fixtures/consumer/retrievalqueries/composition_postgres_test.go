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

type parentMember struct{}

func TestPostgresRetrievalCompositionAndProjectionBoundaries(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
		q := retrievalqueries.QueryRetrievalMembers()
		f := retrievalqueries.MemberFields()
		root, err := q.Where(f.Name.Eq("root")).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		child := query.As[selectedMember](q, "child")
		parent := query.As[parentMember](q, "parent")
		c, p := retrievalqueries.MemberFieldsAt(child.Scope()), retrievalqueries.MemberFieldsAt(parent.Scope())
		joined := query.InnerJoin(child, parent, query.On(c.ParentID, p.ID))
		selected := query.SelectRecord(joined, query.RightScope(joined, parent.Scope()))
		common := query.As[selectedMember](query.CTE("retrieved_members", q), "selected")
		set := q.Where(f.Name.Eq("root")).Union(q.Where(f.Name.Eq("child")))
		locked := q.OrderBy(f.Name.Asc()).Limit(1).ForUpdate()
		lockedCTE := query.AsTransaction[selectedMember](query.TransactionCTE("locked_members", locked), "selected")
		cursor := query.CursorFor(selected)
		cf := retrievalqueries.MemberFieldsAt(cursor.Scope())
		cursor = cursor.UniqueBy(cf.ID.Group())
		projection := retrievalqueries.SelectMemberLabel(q, retrievalqueries.MemberLabelSelection[retrievalqueries.Member]{ID: f.ID.Value(), Name: f.Name.Value()})

		for _, test := range []struct {
			name     string
			hydrated int
			read     func(context.Context) error
		}{
			{"empty", 0, func(ctx context.Context) error { _, err := q.Where(f.Name.Eq("missing")).All(ctx, tx); return err }},
			{"first", 1, func(ctx context.Context) error { _, err := q.First(ctx, tx); return err }},
			{"find", 1, func(ctx context.Context) error { _, err := q.Find(ctx, tx, root.ID); return err }},
			{"count", 0, func(ctx context.Context) error { _, err := q.Count(ctx, tx); return err }},
			{"scalar", 0, func(ctx context.Context) error {
				_, err := query.SelectValue(q, f.Name.Value()).All(ctx, tx)
				return err
			}},
			{"declared-dto", 0, func(ctx context.Context) error {
				labels, err := projection.OrderBy(f.Name.Asc()).All(ctx, tx)
				if err == nil && (len(labels) != 2 || labels[0].Name != "child" || labels[1].Name != "root") {
					return errors.New("DTO selection silently evaluated getters or changed stored fields")
				}
				return err
			}},
			{"numbered-page", 1, func(ctx context.Context) error {
				page, err := q.Paginate(ctx, tx, query.PageRequest{Number: 1, Size: 1})
				if err == nil && (page.Total != 2 || len(page.Items) != 1) {
					return errors.New("numbered page changed")
				}
				return err
			}},
			{"simple-page-lookahead", 2, func(ctx context.Context) error {
				page, err := q.SimplePaginate(ctx, tx, query.PageRequest{Number: 1, Size: 1})
				if err == nil && (!page.HasMore || len(page.Items) != 1) {
					return errors.New("simple page changed")
				}
				return err
			}},
			{"model-cursor-lookahead", 2, func(ctx context.Context) error {
				page, err := q.CursorPaginate(ctx, tx, query.CursorRequest[retrievalqueries.Member]{Size: 1})
				if err == nil && (!page.Next.IsSet() || len(page.Items) != 1) {
					return errors.New("model cursor changed")
				}
				return err
			}},
			{"joined-parent", 1, func(ctx context.Context) error {
				members, err := selected.All(ctx, tx)
				if err == nil && (len(members) != 1 || members[0].Name != "root") {
					return errors.New("self-join selected the wrong model role")
				}
				return err
			}},
			{"joined-cursor", 1, func(ctx context.Context) error {
				_, err := cursor.Paginate(ctx, tx, query.CursorRequest[retrievalqueries.Member]{Size: 1})
				return err
			}},
			{"cte", 2, func(ctx context.Context) error {
				_, err := query.SelectRecord(common, common.Scope()).All(ctx, tx)
				return err
			}},
			{"union", 2, func(ctx context.Context) error { _, err := set.All(ctx, tx); return err }},
			{"locked-model", 1, func(ctx context.Context) error { _, err := locked.All(ctx, tx); return err }},
			{"locked-cte", 1, func(ctx context.Context) error {
				_, err := query.SelectTransactionRecord(lockedCTE, lockedCTE.Scope()).All(ctx, tx)
				return err
			}},
		} {
			var trace retrievalqueries.Trace
			ctx := retrievalqueries.WithTrace(t.Context(), &trace)
			beforeFirst, beforeSecond := stats.Member[0].Load(), stats.Member[1].Load()
			if err := test.read(ctx); err != nil {
				return fmt.Errorf("%s: %w", test.name, err)
			}
			wantFactories := 0
			if test.hydrated > 0 {
				wantFactories = 1
			}
			if len(trace.Calls) != test.hydrated*3 || trace.LocalFactories != wantFactories || stats.Member[0].Load()-beforeFirst != int64(wantFactories) || stats.Member[1].Load()-beforeSecond != int64(wantFactories) {
				return fmt.Errorf("%s lost retrieval ownership: callbacks=%d local factories=%d", test.name, len(trace.Calls), trace.LocalFactories)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
