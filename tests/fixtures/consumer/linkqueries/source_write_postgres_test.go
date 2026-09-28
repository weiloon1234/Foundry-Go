package linkqueries_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type sourceMemberAlias struct{}
type sourceArchiveAlias struct{}

func sourceMembers(t *testing.T, tx *database.Tx) ([]linkqueries.Member, error) {
	t.Helper()
	members, err := linkqueries.QueryLinkMembers().CreateMany(t.Context(), tx, []linkqueries.MemberDraft{
		linkqueries.MemberDraft{}.SetName("original-a"), linkqueries.MemberDraft{}.SetName("original-b"),
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
		if _, err := linkqueries.QueryLinkArchives().Create(t.Context(), tx, draft.SetMemberID(member.ID).SetName("copied-"+member.Name)); err != nil {
			return err
		}
	}
	if duplicate {
		_, err := linkqueries.QueryLinkArchives().Create(t.Context(), tx, draft.SetMemberID(members[0].ID).SetName("ambiguous"))
		return err
	}
	return nil
}

func TestPostgresSourceUpdateAmbiguityRollsBackBothTerminals(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, true); err != nil {
			return err
		}
		a := linkqueries.ArchiveFields()
		m := linkqueries.MemberFields()
		plan := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).MatchID(a.MemberID.Value()).SelectName(a.Name.Value())
		if n, err := plan.Exec(t.Context(), wrappedTransactor{tx}); !errors.Is(err, database.TooManyRows) || n != 0 {
			return errors.New("count-only source update committed an ambiguous source match")
		}
		if rows, err := plan.Returning(t.Context(), tx, 5); !errors.Is(err, database.TooManyRows) || rows != nil {
			return errors.New("model-returning source update published an ambiguous change")
		}
		for _, member := range members {
			stored, err := linkqueries.QueryLinkMembers().RequireFind(t.Context(), tx, member.ID)
			if err != nil {
				return err
			}
			if stored.Name != member.Name {
				return errors.New("failed source update did not roll back every affected model")
			}
		}
		// An excluded target cannot make the remaining write ambiguous.
		selected := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers().Where(m.ID.Eq(members[1].ID)), linkqueries.QueryLinkArchives()).MatchID(a.MemberID.Value()).SelectName(a.Name.Value())
		rows, err := selected.Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != members[1].ID || rows[0].Name != "copied-original-b" {
			return errors.New("destination scope was not preserved")
		}
		// Literal-only changes have the same value for every match, so duplicates are valid.
		fixed := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).MatchID(a.MemberID.Value()).Values(linkqueries.MemberDraft{}.SetName("fixed"))
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
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, true); err != nil {
			return err
		}
		a := linkqueries.ArchiveFields()
		m := linkqueries.MemberFields()
		// The entire duplicate group has two rows, but this explicit window contains one.
		source := linkqueries.QueryLinkArchives().Where(a.MemberID.Eq(members[0].ID)).OrderBy(a.Name.Asc()).Limit(1)
		rows, err := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), source).MatchID(a.MemberID.Value()).SelectName(a.Name.Value()).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "ambiguous" {
			return errors.New("source cardinality was counted before its explicit window")
		}
		// A bounded terminal never silently limits which selected targets are written.
		before, err := linkqueries.QueryLinkMembers().OrderBy(m.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		fixed := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).MatchNullableID(query.Nullable(a.MemberID.Value())).Values(linkqueries.MemberDraft{}.SetName("too-many"))
		if rows, err := fixed.Returning(t.Context(), tx, 1); !errors.Is(err, database.TooManyRows) || rows != nil {
			return errors.New("source returning silently truncated multiple affected models")
		}
		after, err := linkqueries.QueryLinkMembers().OrderBy(m.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		for i := range before {
			if before[i].ID != after[i].ID || before[i].Name != after[i].Name {
				return errors.New("return bound failure did not roll back")
			}
		}
		// A nullable source key really is allowed to be NULL, rather than just a widened value.
		memberSource := query.As[sourceMemberAlias](linkqueries.QueryLinkMembers(), "source_members")
		archiveSource := query.As[sourceArchiveAlias](linkqueries.QueryLinkArchives().Where(a.Tag.Eq("missing")), "source_archives")
		memberFields := linkqueries.MemberFieldsAt(memberSource.Scope())
		joinedArchiveFields := linkqueries.ArchiveFieldsAt(archiveSource.Scope())
		missing := query.LeftJoin(memberSource, archiveSource, query.On(memberFields.ID, joinedArchiveFields.MemberID))
		archiveFields := linkqueries.ArchiveNullableFieldsAt(query.NullableRightScope(missing, archiveSource.Scope()))
		n, err := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), missing).MatchNullableID(archiveFields.MemberID.Value()).Values(linkqueries.MemberDraft{}.SetName("must-not-match")).Exec(t.Context(), tx)
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
	runLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		membership, err := linkqueries.QueryLinkMemberships().Create(t.Context(), tx, linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole("reader"))
		if err != nil {
			return err
		}
		clock.Advance(time.Hour)
		ctx := linkqueries.WithTrace(t.Context(), trace)
		fields := linkqueries.MembershipFields()
		rows, err := linkqueries.UpdateMembershipFrom(linkqueries.QueryLinkMemberships(), linkqueries.QueryLinkMemberships()).MatchID(fields.ID.Value()).Values(linkqueries.MembershipDraft{}.SetRole(" EDITOR ")).Returning(ctx, wrappedTransactor{tx}, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != membership.ID || rows[0].Role != "editor" || rows[0].CreatedAt != membership.CreatedAt || rows[0].UpdatedAt.UTC() != clock.Now().UTC().Truncate(time.Microsecond) || len(trace.Steps) != 0 || trace.Reads != 0 {
			return errors.New("source update bypassed setters/clocks or dispatched per-model observers")
		}
		// Natural primary keys retain their named Go type across the source match.
		n, err := linkqueries.DeleteGroupUsing(linkqueries.QueryLinkGroups(), linkqueries.QueryLinkMemberships()).MatchCode(fields.GroupCode.Value()).Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("source deletion did not match natural key")
		}
		g := linkqueries.GroupFields()
		n, err = linkqueries.DeleteGroupUsing(linkqueries.QueryLinkGroups().WithTrashed(), linkqueries.QueryLinkGroups().WithTrashed()).MatchCode(g.Code.Value()).Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("ordinary source deletion re-deleted trashed model")
		}
		deleted, err := linkqueries.DeleteMembershipUsing(linkqueries.QueryLinkMemberships(), linkqueries.QueryLinkMemberships()).MatchID(fields.ID.Value()).Returning(ctx, tx, 1)
		if err != nil {
			return err
		}
		if len(deleted) != 1 || deleted[0].DeletedAt.IsNull() {
			return errors.New("source soft deletion returned an active model")
		}
		n, err = linkqueries.ForceDeleteMembershipUsing(linkqueries.QueryLinkMemberships().WithTrashed(), linkqueries.QueryLinkMemberships().WithTrashed()).MatchID(fields.ID.Value()).Exec(ctx, tx)
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
	runLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		members, err := sourceMembers(t, tx)
		if err != nil {
			return err
		}
		if err := sourceArchives(t, tx, members, false); err != nil {
			return err
		}
		a := linkqueries.ArchiveFields()
		m := linkqueries.MemberFields()
		cte := query.CTE("foundry_write_source", linkqueries.QueryLinkArchives().Where(a.MemberID.Eq(members[0].ID)))
		aliased := query.As[sourceArchiveAlias](cte, "foundry_selected_rows")
		fields := linkqueries.ArchiveFieldsAt(aliased.Scope())
		if _, err := linkqueries.QueryLinkMembers().Update(t.Context(), tx, members[0].ID, linkqueries.MemberDraft{}.SetAlias("old")); err != nil {
			return err
		}
		rows, err := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), aliased).MatchID(fields.MemberID.Value()).SelectName(fields.Name.Value()).SelectAlias(fields.Alias.Value()).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "copied-original-a" || !rows[0].Alias.IsNull() {
			return errors.New("CTE source update lost stored nullable fields")
		}
		memberSource := query.As[sourceMemberAlias](linkqueries.QueryLinkMembers().Where(m.ID.Eq(members[0].ID)), "members_to_copy")
		archiveSource := query.As[sourceArchiveAlias](linkqueries.QueryLinkArchives(), "archives_to_change")
		mf := linkqueries.MemberFieldsAt(memberSource.Scope())
		af := linkqueries.ArchiveFieldsAt(archiveSource.Scope())
		joined := query.InnerJoin(memberSource, archiveSource, query.On(mf.ID, af.MemberID))
		left := linkqueries.MemberFieldsAt(query.LeftScope(joined, memberSource.Scope()))
		right := linkqueries.ArchiveFieldsAt(query.RightScope(joined, archiveSource.Scope()))
		clock.Advance(time.Hour)
		archives, err := linkqueries.UpdateArchiveFrom(linkqueries.QueryLinkArchives(), joined).MatchID(right.ID.Value()).SelectName(left.Name.Value()).Values(linkqueries.ArchiveDraft{}.SetTag(linkqueries.ArchiveTagInput{Text: " JOINED "})).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(archives) != 1 || archives[0].MemberID != members[0].ID || archives[0].Name != rows[0].Name || archives[0].Tag != "joined" || archives[0].UpdatedAt.UTC() != clock.Now().UTC().Truncate(time.Microsecond) {
			return errors.New("joined source lost model identity, SQL field, distinct setter input or clock")
		}
		// A model without soft deletion uses physical DELETE USING and returns old values.
		removed, err := linkqueries.DeleteArchiveUsing(linkqueries.QueryLinkArchives(), linkqueries.QueryLinkArchives().Where(a.MemberID.Eq(members[0].ID))).MatchID(a.ID.Value()).Returning(t.Context(), tx, 1)
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
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
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
		a := linkqueries.ArchiveFields()
		plan := linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).MatchID(a.MemberID.Value()).SelectName(a.Name.Value())
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
		untouched, err := linkqueries.QueryLinkMembers().RequireFind(t.Context(), tx, members[0].ID)
		if err != nil {
			return err
		}
		if untouched.Name != members[0].Name {
			return errors.New("suppressed source update changed the model")
		}
		return nil
	})
}
