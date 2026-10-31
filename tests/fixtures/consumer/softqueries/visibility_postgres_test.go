package softqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type childAlias struct{}
type parentAlias struct{}

func TestPostgresSoftDeleteRelationsAggregatesAndOuterJoins(t *testing.T) {
	runSoft(t, func(tx *database.Tx, _ *testkit.Clock) error {
		ctx := t.Context()
		q, f := softqueries.Members.Query(), softqueries.Members
		parent, err := orm.Insert(ctx, tx, softqueries.Members.Table, orm.InputTextSet(softqueries.Members.Name, "parent"))
		if err != nil {
			return err
		}
		active, err := orm.Insert(ctx, tx, softqueries.Members.Table, orm.InputTextSet(softqueries.Members.Name, "child-active"), orm.NullableColumnSet(softqueries.Members.ParentID, parent.ID))
		if err != nil {
			return err
		}
		deleted, err := orm.Insert(ctx, tx, softqueries.Members.Table, orm.InputTextSet(softqueries.Members.Name, "child-deleted"), orm.NullableColumnSet(softqueries.Members.ParentID, parent.ID))
		if err != nil {
			return err
		}
		if _, err := orm.Delete(ctx, tx, q, softqueries.Members.Key(), deleted.ID); err != nil {
			return err
		}
		groups := softqueries.Groups.Query()
		group, err := orm.Insert(ctx, tx, softqueries.Groups.Table, orm.KeySet(softqueries.Groups.Key(), "active"), orm.TextSet(softqueries.Groups.Name, "active"))
		if err != nil {
			return err
		}
		hidden, err := orm.Insert(ctx, tx, softqueries.Groups.Table, orm.KeySet(softqueries.Groups.Key(), "deleted"), orm.TextSet(softqueries.Groups.Name, "deleted"))
		if err != nil {
			return err
		}
		if _, err := orm.Delete(ctx, tx, groups, softqueries.Groups.Key(), hidden.Code); err != nil {
			return err
		}
		pivots := softqueries.Memberships.Query()
		if _, err := orm.Insert(ctx, tx, softqueries.Memberships.Table, orm.ColumnSet(softqueries.Memberships.MemberID, parent.ID), orm.TextSet(softqueries.Memberships.GroupCode, group.Code)); err != nil {
			return err
		}
		if _, err := orm.Insert(ctx, tx, softqueries.Memberships.Table, orm.ColumnSet(softqueries.Memberships.MemberID, parent.ID), orm.TextSet(softqueries.Memberships.GroupCode, hidden.Code)); err != nil {
			return err
		}
		pivot, err := orm.Insert(ctx, tx, softqueries.Memberships.Table, orm.ColumnSet(softqueries.Memberships.MemberID, parent.ID), orm.TextSet(softqueries.Memberships.GroupCode, group.Code))
		if err != nil {
			return err
		}
		if _, err := orm.Delete(ctx, tx, pivots, softqueries.Memberships.Key(), pivot.ID); err != nil {
			return err
		}
		r := softqueries.MemberLinks
		loaded, err := orm.Require(ctx, tx, q.With(r.Children, softqueries.MemberCounts.Children).Where(orm.KeyEq(f.Key(), parent.ID)))
		if err != nil {
			return err
		}
		children, present := loaded.Children.Get()
		count, counted := loaded.ChildCount.Get()
		if !present || len(children) != 1 || children[0].ID != active.ID || !counted || count != 1 {
			return errors.New("direct eager loading or aggregate included deleted children")
		}
		allChildren := r.Children.WithTrashed()
		loaded, err = orm.Require(ctx, tx, q.With(allChildren, softqueries.Members.ChildCount.Using(expr.Related(allChildren, expr.Count[softqueries.Member]()))).Where(orm.KeyEq(f.Key(), parent.ID)))
		if err != nil {
			return err
		}
		children, present = loaded.Children.Get()
		count, counted = loaded.ChildCount.Get()
		if !present || len(children) != 2 || !counted || count != 2 {
			return errors.New("explicit child visibility did not reach eager loading and aggregate")
		}
		scoped := q.Where(orm.KeyEq(f.Key(), parent.ID))
		if count, err := orm.Count(ctx, tx, scoped.Where(orm.Has(r.Children.OnlyTrashed()))); err != nil || count != 1 {
			return errors.New("direct EXISTS lost deleted-child scope")
		}
		for _, test := range []struct {
			descriptor orm.ThroughRelation[softqueries.Member, softqueries.Group, softqueries.Membership]
			expected   int
		}{
			{r.Groups, 1},
			{r.Groups.WithTrashed(), 2},
			{r.Groups.WithTrashedPivot(), 2},
			{r.Groups.WithTrashed().WithTrashedPivot(), 3},
			{r.Groups.OnlyTrashed(), 1},
			{r.Groups.OnlyTrashedPivot(), 1},
			{r.Groups.OnlyTrashed().OnlyTrashedPivot(), 0},
		} {
			loaded, err = orm.Require(ctx, tx, q.With(test.descriptor, softqueries.Members.GroupCount.Using(expr.Related(test.descriptor, expr.Count[softqueries.Group]()))).Where(orm.KeyEq(f.Key(), parent.ID)))
			if err != nil {
				return err
			}
			links, present := loaded.Groups.Get()
			count, counted := loaded.GroupCount.Get()
			if !present || len(links) != test.expected || !counted || count != int64(test.expected) {
				return errors.New("target/pivot visibility differs between eager loading and aggregate")
			}
			matched, err := orm.Exists(ctx, tx, scoped.Where(orm.Has(test.descriptor)))
			if err != nil {
				return err
			}
			if matched != (test.expected > 0) {
				return errors.New("through EXISTS lost independent target/pivot visibility")
			}
		}
		childQuery := q.Where(orm.NullableColumnEq(f.ParentID, parent.ID))
		if count, err := orm.Count(ctx, tx, childQuery); err != nil || count != 1 {
			return errors.New("ordinary count exposed a deleted child")
		}
		if count, err := orm.Count(ctx, tx, childQuery.WithTrashed()); err != nil || count != 2 {
			return errors.New("WithTrashed count lost a child")
		}
		visited := 0
		if err := orm.EachByKey(ctx, tx, childQuery, orm.KeyExpr(f.Key()), 1, func(member softqueries.Member) error {
			visited++
			if member.ID != active.ID {
				return errors.New("keyset iteration exposed a deleted child")
			}
			return nil
		}); err != nil {
			return err
		}
		if visited != 1 {
			return errors.New("keyset iteration lost an active child")
		}
		visited = 0
		if err := orm.EachByKey(ctx, tx, childQuery.WithTrashed(), orm.KeyExpr(f.Key()), 1, func(softqueries.Member) error { visited++; return nil }); err != nil {
			return err
		}
		if visited != 2 {
			return errors.New("keyset iteration discarded explicit visibility")
		}
		labels, err := rows.Read(rows.Select(rows.From(childQuery), softqueries.MemberLabels, rows.Map(softqueries.MemberLabels.ID, expr.Value(orm.KeyExpr(f.Key()))), rows.Map(softqueries.MemberLabels.Name, expr.Value(orm.InputTextExpr(f.Name))))).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(labels) != 1 || labels[0].ID != active.ID {
			return errors.New("DTO projection lost the default deletion scope")
		}
		visible := rows.AsRecord[childAlias](rows.CTE("soft_visible", rows.ModelRecord(childQuery)), "visible")
		visibleRows, err := rows.ReadRecord(rows.SelectRecord(visible, rows.Scope(visible))).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(visibleRows) != 1 || visibleRows[0].ID != active.ID {
			return errors.New("CTE lost its model visibility")
		}
		combined, err := rows.ReadSet(rows.UnionAll(rows.ModelRecord(childQuery), rows.ModelRecord(childQuery.OnlyTrashed()))).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(combined) != 2 {
			return errors.New("set inputs did not retain independent deletion scopes")
		}
		page, err := orm.Paginate(ctx, tx, childQuery, orm.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != active.ID {
			return errors.New("pagination count or row selection exposed a deleted model")
		}
		allPage, err := orm.Paginate(ctx, tx, childQuery.WithTrashed(), orm.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if allPage.Total != 2 || len(allPage.Items) != 1 {
			return errors.New("pagination discarded explicit visibility")
		}
		if _, err := orm.Delete(ctx, tx, q, softqueries.Members.Key(), parent.ID); err != nil {
			return err
		}
		loadedChild, err := orm.Require(ctx, tx, q.With(r.Parent).Where(orm.KeyEq(f.Key(), active.ID)))
		if err != nil {
			return err
		}
		ancestorValue, ancestorLoaded := loadedChild.Parent.Get()
		if !ancestorLoaded || ancestorValue.IsSet() {
			return errors.New("singular relation exposed a deleted parent")
		}
		loadedChild, err = orm.Require(ctx, tx, q.With(r.Parent.WithTrashed()).Where(orm.KeyEq(f.Key(), active.ID)))
		if err != nil {
			return err
		}
		ancestorValue, ancestorLoaded = loadedChild.Parent.Get()
		ancestorModel, ancestorPresent := ancestorValue.Get()
		if !ancestorLoaded || !ancestorPresent || ancestorModel.ID != parent.ID {
			return errors.New("singular relation lost explicit parent visibility")
		}
		child := rows.As[childAlias](childQuery, "child")
		ancestor := rows.As[parentAlias](q, "ancestor")
		childParent := expr.At(rows.Scope(child), orm.NullableColumnExpr(f.ParentID))
		parentKey := expr.At(rows.Scope(ancestor), orm.KeyExpr(f.Key()))
		joined := rows.LeftJoin(child, ancestor, rows.On(childParent, parentKey))
		missing := expr.AtNullable(rows.NullableRightScope(joined, rows.Scope(ancestor)), orm.KeyExpr(f.Key()))
		selected := rows.SelectRecord(joined, rows.LeftScope(joined, rows.Scope(child))).Where(expr.IsNull(missing))
		items, err := rows.ReadRecord(selected).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != active.ID {
			return errors.New("soft-delete alias filtering broke outer-join null preservation")
		}
		ancestor = rows.As[parentAlias](q.WithTrashed(), "ancestor")
		parentKey = expr.At(rows.Scope(ancestor), orm.KeyExpr(f.Key()))
		joined = rows.LeftJoin(child, ancestor, rows.On(childParent, parentKey))
		missing = expr.AtNullable(rows.NullableRightScope(joined, rows.Scope(ancestor)), orm.KeyExpr(f.Key()))
		items, err = rows.ReadRecord(rows.SelectRecord(joined, rows.LeftScope(joined, rows.Scope(child))).Where(expr.IsNull(missing))).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 0 {
			return errors.New("explicit ancestor visibility failed to restore the join match")
		}
		return nil
	})
}
