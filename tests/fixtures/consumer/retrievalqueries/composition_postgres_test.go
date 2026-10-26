package retrievalqueries_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type parentMember struct{}

func TestPostgresRetrievalCompositionAndProjectionBoundaries(t *testing.T) {
	db, stats, namespace := retrievalApp(t)
	err := inSchema(t.Context(), db, namespace, func(tx *database.Tx) error {
		q := retrievalqueries.Members.Query()
		f := retrievalqueries.Members
		root, err := q.Where(f.Name.Eq("root")).Require(t.Context(), tx)
		if err != nil {
			return err
		}
		child := rows.As[selectedMember](q, "child")
		parent := rows.As[parentMember](q, "parent")
		childParent := expr.At(rows.Scope(child), f.ParentID)
		parentKey := expr.At(rows.Scope(parent), f.Key())
		joined := rows.InnerJoin(child, parent, rows.On(childParent, parentKey))
		selected := rows.SelectRecord(joined, rows.RightScope(joined, rows.Scope(parent)))
		common := rows.AsRecord[selectedMember](rows.CTE("retrieved_members", rows.ModelRecord(q)), "selected")
		set := rows.Union(rows.ModelRecord(q.Where(f.Name.Eq("root"))), rows.ModelRecord(q.Where(f.Name.Eq("child"))))
		locked := q.OrderBy(f.Name.Asc()).Limit(1).ForUpdate()
		lockedCTE := rows.AsTransaction[selectedMember](rows.TransactionCTE("locked_members", rows.LockedRecord(locked)), "selected")
		cursor := rows.CursorFor(selected)
		cursorKey := expr.At(cursor.Scope(), f.Key())
		cursor = cursor.UniqueBy(expr.Group(cursorKey))
		projection := rows.Select(rows.From(q), retrievalqueries.MemberLabels, rows.Map(retrievalqueries.MemberLabels.ID, expr.Value(f.Key())), rows.Map(retrievalqueries.MemberLabels.Name, expr.Value(f.Name)))

		for _, test := range []struct {
			name     string
			hydrated int
			read     func(context.Context) error
		}{
			{"empty", 0, func(ctx context.Context) error { _, err := q.Where(f.Name.Eq("missing")).All(ctx, tx); return err }},
			{"first", 1, func(ctx context.Context) error { _, err := q.First(ctx, tx); return err }},
			{"find", 1, func(ctx context.Context) error { _, err := retrievalqueries.Members.Find(ctx, tx, root.ID); return err }},
			{"count", 0, func(ctx context.Context) error { _, err := q.Count(ctx, tx); return err }},
			{"scalar", 0, func(ctx context.Context) error {
				_, err := rows.SelectValue(q, f.Name).All(ctx, tx)
				return err
			}},
			{"declared-dto", 0, func(ctx context.Context) error {
				labels, err := projection.OrderBy(expr.Value(f.Name).Asc()).All(ctx, tx)
				if err == nil && (len(labels) != 2 || labels[0].Name != "child" || labels[1].Name != "root") {
					return errors.New("DTO selection silently evaluated getters or changed stored fields")
				}
				return err
			}},
			{"numbered-page", 1, func(ctx context.Context) error {
				page, err := q.Paginate(ctx, tx, orm.PageRequest{Number: 1, Size: 1})
				if err == nil && (page.Total != 2 || len(page.Items) != 1) {
					return errors.New("numbered page changed")
				}
				return err
			}},
			{"simple-page-lookahead", 2, func(ctx context.Context) error {
				page, err := q.SimplePaginate(ctx, tx, orm.PageRequest{Number: 1, Size: 1})
				if err == nil && (!page.HasMore || len(page.Items) != 1) {
					return errors.New("simple page changed")
				}
				return err
			}},
			{"model-cursor-lookahead", 2, func(ctx context.Context) error {
				page, err := orm.CursorPaginate(ctx, tx, q, orm.CursorRequest[retrievalqueries.Member]{Size: 1})
				if err == nil && (!page.Next.IsSet() || len(page.Items) != 1) {
					return errors.New("model cursor changed")
				}
				return err
			}},
			{"joined-parent", 1, func(ctx context.Context) error {
				members, err := rows.ReadRecord(selected).All(ctx, tx)
				if err == nil && (len(members) != 1 || members[0].Name != "root") {
					return errors.New("self-join selected the wrong model role")
				}
				return err
			}},
			{"joined-cursor", 1, func(ctx context.Context) error {
				_, err := cursor.Paginate(ctx, tx, orm.CursorRequest[retrievalqueries.Member]{Size: 1})
				return err
			}},
			{"cte", 2, func(ctx context.Context) error {
				_, err := rows.ReadRecord(rows.SelectRecord(common, rows.Scope(common))).All(ctx, tx)
				return err
			}},
			{"union", 2, func(ctx context.Context) error { _, err := rows.ReadSet(set).All(ctx, tx); return err }},
			{"locked-model", 1, func(ctx context.Context) error { _, err := locked.All(ctx, tx); return err }},
			{"locked-cte", 1, func(ctx context.Context) error {
				_, err := rows.SelectTransactionRecord(lockedCTE, rows.TransactionScope(lockedCTE)).All(ctx, tx)
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
