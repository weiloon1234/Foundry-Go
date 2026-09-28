package softqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type childAlias struct{}
type parentAlias struct{}

func TestPostgresSoftDeleteRelationsAggregatesAndOuterJoins(t *testing.T) {
	runSoft(t, func(tx *database.Tx, _ *testkit.Clock) error {
		ctx := t.Context()
		q, f := softqueries.QuerySoftMembers(), softqueries.MemberFields()
		parent, err := q.Create(ctx, tx, softqueries.MemberDraft{}.SetName("parent"))
		if err != nil {
			return err
		}
		active, err := q.Create(ctx, tx, softqueries.MemberDraft{}.SetName("child-active").SetParentID(parent.ID))
		if err != nil {
			return err
		}
		deleted, err := q.Create(ctx, tx, softqueries.MemberDraft{}.SetName("child-deleted").SetParentID(parent.ID))
		if err != nil {
			return err
		}
		if _, err := q.Delete(ctx, tx, deleted.ID); err != nil {
			return err
		}
		groups := softqueries.QuerySoftGroups()
		group, err := groups.Create(ctx, tx, softqueries.GroupDraft{}.SetCode("active").SetName("active"))
		if err != nil {
			return err
		}
		hidden, err := groups.Create(ctx, tx, softqueries.GroupDraft{}.SetCode("deleted").SetName("deleted"))
		if err != nil {
			return err
		}
		if _, err := groups.Delete(ctx, tx, hidden.Code); err != nil {
			return err
		}
		pivots := softqueries.QuerySoftMemberships()
		if _, err := pivots.Create(ctx, tx, softqueries.MembershipDraft{}.SetMemberID(parent.ID).SetGroupCode(group.Code)); err != nil {
			return err
		}
		if _, err := pivots.Create(ctx, tx, softqueries.MembershipDraft{}.SetMemberID(parent.ID).SetGroupCode(hidden.Code)); err != nil {
			return err
		}
		pivot, err := pivots.Create(ctx, tx, softqueries.MembershipDraft{}.SetMemberID(parent.ID).SetGroupCode(group.Code))
		if err != nil {
			return err
		}
		if _, err := pivots.Delete(ctx, tx, pivot.ID); err != nil {
			return err
		}
		r, a := softqueries.MemberRelations(), softqueries.MemberAggregates()
		loaded, err := q.With(r.Children, a.ChildCount).RequireFind(ctx, tx, parent.ID)
		if err != nil {
			return err
		}
		children, present := loaded.Children.Get()
		count, counted := loaded.ChildCount.Get()
		if !present || len(children) != 1 || children[0].ID != active.ID || !counted || count != 1 {
			return errors.New("direct eager loading or aggregate included deleted children")
		}
		allChildren := r.Children.WithTrashed()
		loaded, err = q.With(allChildren, a.ChildCount.Using(query.Related(allChildren, query.Count[softqueries.Member]()))).RequireFind(ctx, tx, parent.ID)
		if err != nil {
			return err
		}
		children, present = loaded.Children.Get()
		count, counted = loaded.ChildCount.Get()
		if !present || len(children) != 2 || !counted || count != 2 {
			return errors.New("explicit child visibility did not reach eager loading and aggregate")
		}
		scoped := q.Where(f.ID.Eq(parent.ID))
		if count, err := scoped.WhereHas(r.Children.OnlyTrashed()).Count(ctx, tx); err != nil || count != 1 {
			return errors.New("direct EXISTS lost deleted-child scope")
		}
		for _, test := range []struct {
			descriptor query.ThroughRelation[softqueries.Member, softqueries.Group, softqueries.Membership]
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
			loaded, err = q.With(test.descriptor, a.GroupCount.Using(query.Related(test.descriptor, query.Count[softqueries.Group]()))).RequireFind(ctx, tx, parent.ID)
			if err != nil {
				return err
			}
			links, present := loaded.Groups.Get()
			count, counted := loaded.GroupCount.Get()
			if !present || len(links) != test.expected || !counted || count != int64(test.expected) {
				return errors.New("target/pivot visibility differs between eager loading and aggregate")
			}
			matched, err := scoped.WhereHas(test.descriptor).Exists(ctx, tx)
			if err != nil {
				return err
			}
			if matched != (test.expected > 0) {
				return errors.New("through EXISTS lost independent target/pivot visibility")
			}
		}
		childQuery := q.Where(f.ParentID.Eq(parent.ID))
		if count, err := childQuery.Count(ctx, tx); err != nil || count != 1 {
			return errors.New("ordinary count exposed a deleted child")
		}
		if count, err := childQuery.WithTrashed().Count(ctx, tx); err != nil || count != 2 {
			return errors.New("WithTrashed count lost a child")
		}
		visited := 0
		if err := childQuery.EachByID(ctx, tx, 1, func(member softqueries.Member) error {
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
		if err := childQuery.WithTrashed().EachByID(ctx, tx, 1, func(softqueries.Member) error { visited++; return nil }); err != nil {
			return err
		}
		if visited != 2 {
			return errors.New("keyset iteration discarded explicit visibility")
		}
		labels, err := softqueries.ProjectMemberLabel(childQuery).SelectID(f.ID.Value()).SelectName(f.Name.Value()).Query().All(ctx, tx)
		if err != nil {
			return err
		}
		if len(labels) != 1 || labels[0].ID != active.ID {
			return errors.New("DTO projection lost the default deletion scope")
		}
		visible := query.As[childAlias](query.CTE("soft_visible", childQuery), "visible")
		visibleRows, err := query.SelectRecord(visible, visible.Scope()).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(visibleRows) != 1 || visibleRows[0].ID != active.ID {
			return errors.New("CTE lost its model visibility")
		}
		combined, err := query.UnionAll(childQuery, childQuery.OnlyTrashed()).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(combined) != 2 {
			return errors.New("set inputs did not retain independent deletion scopes")
		}
		page, err := childQuery.Paginate(ctx, tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != active.ID {
			return errors.New("pagination count or row selection exposed a deleted model")
		}
		allPage, err := childQuery.WithTrashed().Paginate(ctx, tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if allPage.Total != 2 || len(allPage.Items) != 1 {
			return errors.New("pagination discarded explicit visibility")
		}
		if _, err := q.Delete(ctx, tx, parent.ID); err != nil {
			return err
		}
		loadedChild, err := q.With(r.Parent).RequireFind(ctx, tx, active.ID)
		if err != nil {
			return err
		}
		ancestorValue, ancestorLoaded := loadedChild.Parent.Get()
		if !ancestorLoaded || ancestorValue.IsSet() {
			return errors.New("singular relation exposed a deleted parent")
		}
		loadedChild, err = q.With(r.Parent.WithTrashed()).RequireFind(ctx, tx, active.ID)
		if err != nil {
			return err
		}
		ancestorValue, ancestorLoaded = loadedChild.Parent.Get()
		ancestorModel, ancestorPresent := ancestorValue.Get()
		if !ancestorLoaded || !ancestorPresent || ancestorModel.ID != parent.ID {
			return errors.New("singular relation lost explicit parent visibility")
		}
		child := query.As[childAlias](childQuery, "child")
		ancestor := query.As[parentAlias](q, "ancestor")
		c, p := softqueries.MemberFieldsAt(child.Scope()), softqueries.MemberFieldsAt(ancestor.Scope())
		joined := query.LeftJoin(child, ancestor, query.On(c.ParentID, p.ID))
		missing := softqueries.MemberNullableFieldsAt(query.NullableRightScope(joined, ancestor.Scope()))
		selected := query.SelectRecord(joined, query.LeftScope(joined, child.Scope())).Where(missing.ID.IsNull())
		rows, err := selected.All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != active.ID {
			return errors.New("soft-delete alias filtering broke outer-join null preservation")
		}
		ancestor = query.As[parentAlias](q.WithTrashed(), "ancestor")
		p = softqueries.MemberFieldsAt(ancestor.Scope())
		joined = query.LeftJoin(child, ancestor, query.On(c.ParentID, p.ID))
		missing = softqueries.MemberNullableFieldsAt(query.NullableRightScope(joined, ancestor.Scope()))
		rows, err = query.SelectRecord(joined, query.LeftScope(joined, child.Scope())).Where(missing.ID.IsNull()).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			return errors.New("explicit ancestor visibility failed to restore the join match")
		}
		return nil
	})
}
