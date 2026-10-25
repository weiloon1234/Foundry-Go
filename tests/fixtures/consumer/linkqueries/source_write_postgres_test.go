package linkqueries_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	sqlrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type sourceMemberAlias struct{}
type sourceArchiveAlias struct{}

func sourceMembers(t *testing.T, tx *database.Tx) ([]linkqueries.Member, error) {
	t.Helper()
	members, err := linkqueries.Members.InsertMany(t.Context(), tx, [][]orm.Assignment[linkqueries.Member]{
		[]orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("original-a")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("original-b")},
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(members, func(a, b linkqueries.Member) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return members, nil
}

func sourceArchives(t *testing.T, tx *database.Tx, members []linkqueries.Member, duplicate bool) error {
	t.Helper()
	draft := archiveValues(t)
	for _, member := range members {
		if _, err := linkqueries.Archives.Insert(t.Context(), tx, append(slices.Clone(draft), linkqueries.Archives.MemberID.Set(member.ID), linkqueries.Archives.Name.Set("copied-"+member.Name))...); err != nil {
			return err
		}
	}
	if duplicate {
		_, err := linkqueries.Archives.Insert(t.Context(), tx, append(slices.Clone(draft), linkqueries.Archives.MemberID.Set(members[0].ID), linkqueries.Archives.Name.Set("ambiguous"))...)
		return err
	}
	return nil
}

func TestPostgresSourceUpdateAmbiguityRollsBackBothTerminals(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, true); err != nil {
			return err
		}
		a := linkqueries.Archives
		m := linkqueries.Members
		plan := sqlrows.UpdateFrom(linkqueries.Members.Query(), sqlrows.From(linkqueries.Archives.Query())).Match(sqlrows.MatchSource(linkqueries.Members.Key(), expr.Value(a.MemberID))).Select(sqlrows.MapUpdate(linkqueries.Members.Name, expr.Value(a.Name)))
		if n, err := plan.Exec(t.Context(), wrappedTransactor{tx}); !errors.Is(err, database.TooManyRows) || n != 0 {
			return errors.New("count-only source update committed an ambiguous source match")
		}
		if rows, err := plan.Returning(t.Context(), tx, 5); !errors.Is(err, database.TooManyRows) || rows != nil {
			return errors.New("model-returning source update published an ambiguous change")
		}
		for _, member := range members {
			stored, err := linkqueries.Members.Require(t.Context(), tx, member.ID)
			if err != nil {
				return err
			}
			if stored.Name != member.Name {
				return errors.New("failed source update did not roll back every affected model")
			}
		}
		// An excluded target cannot make the remaining write ambiguous.
		selected := sqlrows.UpdateFrom(linkqueries.Members.Query().Where(m.ID.Eq(members[1].ID)), sqlrows.From(linkqueries.Archives.Query())).Match(sqlrows.MatchSource(linkqueries.Members.Key(), expr.Value(a.MemberID))).Select(sqlrows.MapUpdate(linkqueries.Members.Name, expr.Value(a.Name)))
		rows, err := selected.Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != members[1].ID || rows[0].Name != "copied-original-b" {
			return errors.New("destination scope was not preserved")
		}
		// Literal-only changes have the same value for every match, so duplicates are valid.
		fixed := sqlrows.UpdateFrom(linkqueries.Members.Query(), sqlrows.From(linkqueries.Archives.Query())).Match(sqlrows.MatchSource(linkqueries.Members.Key(), expr.Value(a.MemberID))).Values(linkqueries.Members.Name.Set("fixed"))
		n, err := fixed.Exec(t.Context(), tx)
		if err != nil {
			return err
		}
		if n != 2 {
			return errors.New("duplicate source rows multiplied fixed update count")
		}
		return nil
	})
}

func TestPostgresSourceWriteWindowsNullsAndReturningBounds(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, true); err != nil {
			return err
		}
		a := linkqueries.Archives
		m := linkqueries.Members
		// The entire duplicate group has two rows, but this explicit window contains one.
		source := linkqueries.Archives.Query().Where(a.MemberID.Eq(members[0].ID)).OrderBy(a.Name.Asc()).Limit(1)
		rows, err := sqlrows.UpdateFrom(linkqueries.Members.Query(), sqlrows.From(source)).Match(sqlrows.MatchSource(linkqueries.Members.Key(), expr.Value(a.MemberID))).Select(sqlrows.MapUpdate(linkqueries.Members.Name, expr.Value(a.Name))).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "ambiguous" {
			return errors.New("source cardinality was counted before its explicit window")
		}
		// A bounded terminal never silently limits which selected targets are written.
		before, err := linkqueries.Members.Query().OrderBy(m.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		fixed := sqlrows.UpdateFrom(linkqueries.Members.Query(), sqlrows.From(linkqueries.Archives.Query())).Match(sqlrows.MatchNullableSource(linkqueries.Members.Key(), expr.NullableRow(expr.Value(a.MemberID)))).Values(linkqueries.Members.Name.Set("too-many"))
		if rows, err := fixed.Returning(t.Context(), tx, 1); !errors.Is(err, database.TooManyRows) || rows != nil {
			return errors.New("source returning silently truncated multiple affected models")
		}
		after, err := linkqueries.Members.Query().OrderBy(m.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		for i := range before {
			if before[i].ID != after[i].ID || before[i].Name != after[i].Name {
				return errors.New("return bound failure did not roll back")
			}
		}
		// A nullable source key really is allowed to be NULL, rather than just a widened value.
		memberSource := sqlrows.As[sourceMemberAlias](linkqueries.Members.Query(), "source_members")
		archiveSource := sqlrows.As[sourceArchiveAlias](linkqueries.Archives.Query().Where(a.Tag.Eq("missing")), "source_archives")

		missing := sqlrows.LeftJoin(memberSource, archiveSource, sqlrows.On(expr.At(sqlrows.Scope(memberSource), linkqueries.Members.ID), expr.At(sqlrows.Scope(archiveSource), linkqueries.Archives.MemberID)))

		n, err := sqlrows.UpdateFrom(linkqueries.Members.Query(), missing).Match(sqlrows.MatchNullableSource(linkqueries.Members.Key(), expr.AtNullable(sqlrows.NullableRightScope(missing, sqlrows.Scope(archiveSource)), linkqueries.Archives.MemberID))).Values(linkqueries.Members.Name.Set("must-not-match")).Exec(t.Context(), tx)
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("NULL outer-join keys matched models")
		}
		return nil
	})
}

func TestPostgresSourceWriteSharesSettersClocksAndDeletionVisibility(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		membership, err := linkqueries.Memberships.Insert(t.Context(), tx, linkqueries.Memberships.MemberID.Set(member.ID), linkqueries.Memberships.GroupCode.Set(group.Code), linkqueries.Memberships.Role.Set("reader"))
		if err != nil {
			return err
		}
		clock.Advance(time.Hour)
		ctx := linkqueries.WithTrace(t.Context(), trace)
		fields := linkqueries.Memberships
		rows, err := sqlrows.UpdateFrom(linkqueries.Memberships.Query(), sqlrows.From(linkqueries.Memberships.Query())).Match(sqlrows.MatchSource(linkqueries.Memberships.Key(), expr.Value(fields.Key()))).Values(linkqueries.Memberships.Role.Set(" EDITOR ")).Returning(ctx, wrappedTransactor{tx}, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != membership.ID || rows[0].Role != "editor" || rows[0].CreatedAt != membership.CreatedAt || rows[0].UpdatedAt.UTC() != clock.Now().UTC().Truncate(time.Microsecond) || len(trace.Steps) != 0 || trace.Reads != 0 {
			return errors.New("source update bypassed setters/clocks or dispatched per-model observers")
		}
		// Natural primary keys retain their named Go type across the source match.
		n, err := sqlrows.DeleteUsing(linkqueries.Groups.Query(), sqlrows.From(linkqueries.Memberships.Query())).Match(sqlrows.MatchSource(linkqueries.Groups.Key(), expr.Value(fields.GroupCode))).Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("source deletion did not match natural key")
		}
		g := linkqueries.Groups
		n, err = sqlrows.DeleteUsing(linkqueries.Groups.Query().WithTrashed(), sqlrows.From(linkqueries.Groups.Query().WithTrashed())).Match(sqlrows.MatchSource(linkqueries.Groups.Key(), expr.Value(g.Key()))).Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("ordinary source deletion re-deleted trashed model")
		}
		deleted, err := sqlrows.DeleteUsing(linkqueries.Memberships.Query(), sqlrows.From(linkqueries.Memberships.Query())).Match(sqlrows.MatchSource(linkqueries.Memberships.Key(), expr.Value(fields.Key()))).Returning(ctx, tx, 1)
		if err != nil {
			return err
		}
		if len(deleted) != 1 || deleted[0].DeletedAt.IsNull() {
			return errors.New("source soft deletion returned an active model")
		}
		n, err = sqlrows.ForceDeleteUsing(linkqueries.Memberships.Query().WithTrashed(), linkqueries.Memberships.Soft, sqlrows.From(linkqueries.Memberships.Query().WithTrashed())).Match(sqlrows.MatchSource(linkqueries.Memberships.Key(), expr.Value(fields.Key()))).Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("source force deletion lost explicit visibility")
		}
		if len(trace.Steps) != 0 || trace.Reads != 0 {
			return errors.New("source deletions dispatched model hooks or retrieval observers")
		}
		return nil
	})
}

func TestPostgresSourceUpdateJoinsCTEsAndStoredNullableValues(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, false); err != nil {
			return err
		}
		a := linkqueries.Archives
		m := linkqueries.Members
		cte := sqlrows.CTE("foundry_write_source", sqlrows.ModelRecord(linkqueries.Archives.Query().Where(a.MemberID.Eq(members[0].ID))))
		aliased := sqlrows.AsRecord[sourceArchiveAlias](cte, "foundry_selected_rows")

		if _, err := linkqueries.Members.Update(t.Context(), tx, members[0].ID, linkqueries.Members.Alias.Set("old")); err != nil {
			return err
		}
		rows, err := sqlrows.UpdateFrom(linkqueries.Members.Query(), aliased).Match(sqlrows.MatchSource(linkqueries.Members.Key(), expr.At(sqlrows.Scope(aliased), linkqueries.Archives.MemberID))).Select(sqlrows.MapUpdate(linkqueries.Members.Name, expr.At(sqlrows.Scope(aliased), linkqueries.Archives.Name))).Select(sqlrows.MapUpdate(linkqueries.Members.Alias, expr.At(sqlrows.Scope(aliased), linkqueries.Archives.Alias))).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "copied-original-a" || !rows[0].Alias.IsNull() {
			return errors.New("CTE source update lost stored nullable fields")
		}
		memberSource := sqlrows.As[sourceMemberAlias](linkqueries.Members.Query().Where(m.ID.Eq(members[0].ID)), "members_to_copy")
		archiveSource := sqlrows.As[sourceArchiveAlias](linkqueries.Archives.Query(), "archives_to_change")

		joined := sqlrows.InnerJoin(memberSource, archiveSource, sqlrows.On(expr.At(sqlrows.Scope(memberSource), linkqueries.Members.ID), expr.At(sqlrows.Scope(archiveSource), linkqueries.Archives.MemberID)))

		clock.Advance(time.Hour)
		archives, err := sqlrows.UpdateFrom(linkqueries.Archives.Query(), joined).Match(sqlrows.MatchSource(linkqueries.Archives.Key(), expr.At(sqlrows.RightScope(joined, sqlrows.Scope(archiveSource)), linkqueries.Archives.ID))).Select(sqlrows.MapUpdate(linkqueries.Archives.Name, expr.At(sqlrows.LeftScope(joined, sqlrows.Scope(memberSource)), linkqueries.Members.Name))).Values(linkqueries.Archives.Tag.Set(linkqueries.ArchiveTagInput{Text: " JOINED "})).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(archives) != 1 || archives[0].MemberID != members[0].ID || archives[0].Name != rows[0].Name || archives[0].Tag != "joined" || archives[0].UpdatedAt.UTC() != clock.Now().UTC().Truncate(time.Microsecond) {
			return errors.New("joined source lost model identity, SQL field, distinct setter input or clock")
		}
		// A model without soft deletion uses physical DELETE USING and returns old values.
		removed, err := sqlrows.DeleteUsing(linkqueries.Archives.Query(), sqlrows.From(linkqueries.Archives.Query().Where(a.MemberID.Eq(members[0].ID)))).Match(sqlrows.MatchSource(linkqueries.Archives.Key(), expr.Value(a.ID))).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(removed) != 1 || removed[0].ID != archives[0].ID || removed[0].Tag != "joined" {
			return errors.New("physical source deletion lost complete previous values")
		}
		return nil
	})
}

func TestPostgresSourceUpdateTriggerSuppressionCountsOnlyAffectedModels(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, true); err != nil {
			return err
		}
		for _, ddl := range []string{
			`CREATE FUNCTION source_update_suppress() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.name = 'original-a' THEN RETURN NULL; END IF; RETURN NEW; END $$`,
			`CREATE TRIGGER source_update_suppress BEFORE UPDATE ON link_members FOR EACH ROW EXECUTE FUNCTION source_update_suppress()`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		a := linkqueries.Archives
		plan := sqlrows.UpdateFrom(linkqueries.Members.Query(), sqlrows.From(linkqueries.Archives.Query())).Match(sqlrows.MatchSource(linkqueries.Members.Key(), expr.Value(a.MemberID))).Select(sqlrows.MapUpdate(linkqueries.Members.Name, expr.Value(a.Name)))
		n, err := plan.Exec(t.Context(), tx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("count included a trigger-suppressed model")
		}
		rows, err := plan.Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != members[1].ID {
			return errors.New("returning counted a suppressed ambiguous source group")
		}
		untouched, err := linkqueries.Members.Require(t.Context(), tx, members[0].ID)
		if err != nil {
			return err
		}
		if untouched.Name != members[0].Name {
			return errors.New("suppressed source update changed the model")
		}
		return nil
	})
}
