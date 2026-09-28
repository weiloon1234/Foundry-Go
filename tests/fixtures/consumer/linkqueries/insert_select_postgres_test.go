package linkqueries_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func archiveValues(t *testing.T) linkqueries.ArchiveDraft {
	t.Helper()
	amount, err := decimal.Parse("12345678901234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := value.NewJSON([]string{"first", "quoted '"})
	if err != nil {
		t.Fatal(err)
	}
	return linkqueries.ArchiveDraft{}.SetTag(linkqueries.ArchiveTagInput{Text: " BATCH "}).SetAmount(amount).SetPayload(payload)
}

func archiveMembers(source linkqueries.MemberQuery) linkqueries.ArchiveInsertFromBuilder[linkqueries.Member] {
	fields := linkqueries.MemberFields()
	return linkqueries.InsertArchiveFrom(source).SelectMemberID(fields.ID.Value()).SelectName(fields.Name.Value()).SelectAlias(fields.Alias.Value())
}

func TestPostgresInsertSelectPreservesTypesDefaultsAndBulkSemantics(t *testing.T) {
	trace := &linkqueries.Trace{}
	runLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		members, err := linkqueries.QueryLinkMembers().CreateMany(t.Context(), tx, []linkqueries.MemberDraft{
			linkqueries.MemberDraft{}.SetName("alpha"), linkqueries.MemberDraft{}.SetName("beta").SetAlias("Bee"),
		})
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		old := clock.Now().Add(-24 * time.Hour).UTC().Truncate(time.Microsecond)
		draft := archiveValues(t).SetCreatedAt(old)
		inserted, err := archiveMembers(linkqueries.QueryLinkMembers()).Values(draft).Returning(ctx, wrappedTransactor{tx}, 2)
		if err != nil {
			return err
		}
		if len(inserted) != 2 || len(trace.Steps) != 0 || trace.Reads != 0 {
			return errors.New("insert selection dispatched per-model hooks or lost complete results")
		}
		originals := make(map[model.ID[linkqueries.Member]]linkqueries.Member)
		for _, member := range members {
			originals[member.ID] = member
		}
		ids := make(map[model.ID[linkqueries.Archive]]bool)
		amount, _ := draft.Amount().Get()
		for _, archive := range inserted {
			member, ok := originals[archive.MemberID]
			payload, err := archive.Payload.Decode()
			if err != nil {
				return err
			}
			if !ok || archive.Name != member.Name || archive.Alias != member.Alias || archive.Counter != 42 || archive.Tag != "batch" || archive.Amount != amount || !slices.Equal(payload, []string{"first", "quoted '"}) || archive.CreatedAt != old || archive.UpdatedAt.UTC() != clock.Now().UTC().Truncate(time.Microsecond) {
				return errors.New("insert selection changed typed identity, NULL, default, exact values or model timestamps")
			}
			if archive.ID == (model.ID[linkqueries.Archive]{}) || ids[archive.ID] {
				return errors.New("database did not assign separate archive identities")
			}
			ids[archive.ID] = true
		}
		if draft.ID().IsSet() || draft.UpdatedAt().IsSet() {
			return errors.New("insert preparation changed the source draft")
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if text := fmt.Sprintf(format, archiveMembers(linkqueries.QueryLinkMembers()).Values(draft)); text != "model insert from query" {
				return errors.New("generated insertion diagnostics exposed captured inputs")
			}
		}
		// Ordinary creation still dispatches the same model's configured hook.
		ordinary := draft.SetMemberID(members[0].ID).SetName("ordinary")
		if _, err := linkqueries.QueryLinkArchives().Create(ctx, tx, ordinary); !errors.Is(err, linkqueries.ErrVeto) {
			return errors.New("fixture did not exercise real creation hooks")
		}
		return nil
	})
	if len(trace.Committed) != 0 {
		t.Fatal("set-based insert registered per-model after-commit work")
	}
}

func TestPostgresInsertSelectBoundsVetoAndOuterRollback(t *testing.T) {
	for _, mode := range []string{"return_bound", "mutator_veto", "outer_rollback", "unique_violation"} {
		t.Run(mode, func(t *testing.T) {
			runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				if _, err := linkqueries.QueryLinkMembers().CreateMany(t.Context(), tx, []linkqueries.MemberDraft{linkqueries.MemberDraft{}.SetName("one"), linkqueries.MemberDraft{}.SetName("two")}); err != nil {
					return err
				}
				draft := archiveValues(t)
				plan := archiveMembers(linkqueries.QueryLinkMembers()).Values(draft)
				switch mode {
				case "return_bound":
					rows, err := plan.Returning(t.Context(), tx, 1)
					if !errors.Is(err, database.TooManyRows) || rows != nil {
						return errors.New("excess insertion results did not fail atomically")
					}
				case "mutator_veto":
					count, err := plan.Values(draft.SetTag(linkqueries.ArchiveTagInput{Text: "reject"})).Exec(t.Context(), tx)
					if !errors.Is(err, linkqueries.ErrVeto) || count != 0 {
						return errors.New("literal setter veto was bypassed")
					}
				case "unique_violation":
					id, err := model.NewID[linkqueries.Archive]()
					if err != nil {
						return err
					}
					count, err := plan.Values(draft.SetID(id)).Exec(t.Context(), tx)
					if !errors.Is(err, database.UniqueViolation) || count != 0 {
						return errors.New("repeated literal primary key did not fail atomically")
					}
				case "outer_rollback":
					err := tx.Transaction(t.Context(), func(inner *database.Tx) error {
						count, err := plan.Exec(t.Context(), wrappedTransactor{inner})
						if err != nil {
							return err
						}
						if count != 2 {
							return errors.New("count-only insert lost selected rows")
						}
						return linkqueries.ErrVeto
					})
					if !errors.Is(err, linkqueries.ErrVeto) {
						return errors.New("insert selection lost outer transaction rollback")
					}
				}
				count, err := linkqueries.QueryLinkArchives().Count(t.Context(), tx)
				if err != nil || count != 0 {
					return errors.New("failed insert retained rows or damaged reusable parent transaction")
				}
				// The same immutable plan remains usable after the failed attempt.
				count, err = plan.Exec(t.Context(), tx)
				if err != nil || count != 2 {
					return errors.New("insert selection could not be reused after failure")
				}
				return nil
			})
		})
	}
}

type archiveSourceAlias struct{}

func TestPostgresInsertSelectScopesWindowsCTEsAndEmptySources(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := linkqueries.QueryLinkMembers().CreateMany(t.Context(), tx, []linkqueries.MemberDraft{
			linkqueries.MemberDraft{}.SetName("alpha"), linkqueries.MemberDraft{}.SetName("beta"), linkqueries.MemberDraft{}.SetName("gamma"),
		})
		if err != nil {
			return err
		}
		fields := linkqueries.MemberFields()
		alpha := slices.IndexFunc(members, func(member linkqueries.Member) bool { return member.Name == "alpha" })
		if alpha < 0 {
			return errors.New("fixture did not return its alpha member")
		}
		if _, err := linkqueries.QueryLinkMembers().Delete(t.Context(), tx, members[alpha].ID); err != nil {
			return err
		}
		source := linkqueries.QueryLinkMembers().OrderBy(fields.Name.Asc()).Offset(1).Limit(1)
		inserted, err := archiveMembers(source).Values(archiveValues(t)).Returning(t.Context(), tx, 2)
		if err != nil || len(inserted) != 1 || inserted[0].Name != "gamma" {
			return errors.New("source soft visibility or ordered window changed")
		}
		cte := query.CTE("archivable", linkqueries.QueryLinkMembers().OnlyTrashed())
		aliased := query.As[archiveSourceAlias](cte, "archived_member")
		f := linkqueries.MemberFieldsAt(aliased.Scope())
		count, err := linkqueries.InsertArchiveFrom(aliased).SelectMemberID(f.ID.Value()).SelectName(f.Name.Value()).SelectAlias(f.Alias.Value()).Values(archiveValues(t)).Exec(t.Context(), tx)
		if err != nil || count != 1 {
			return errors.New("insert selection lost aliased CTE or explicit deleted scope")
		}
		empty := archiveMembers(linkqueries.QueryLinkMembers().Where(fields.Name.Eq("absent"))).Values(archiveValues(t))
		rows, err := empty.Returning(t.Context(), tx, 1)
		if err != nil || len(rows) != 0 {
			return errors.New("empty source did not return an empty model slice")
		}
		count, err = empty.Exec(t.Context(), tx)
		if err != nil || count != 0 {
			return errors.New("empty count-only source affected rows")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := archiveMembers(linkqueries.QueryLinkMembers()).Values(archiveValues(t)).Exec(ctx, tx); !errors.Is(err, context.Canceled) {
			return errors.New("canceled insertion started work")
		}
		return nil
	})
}

func TestPostgresInsertSelectReturningDoesNotTruncateSuppressedSourceRows(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		for _, ddl := range []string{
			`CREATE FUNCTION skip_archive_rows() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name LIKE 'skip%' THEN RETURN NULL; END IF; RETURN NEW; END $$`,
			`CREATE TRIGGER skip_archive_rows BEFORE INSERT ON link_archives FOR EACH ROW EXECUTE FUNCTION skip_archive_rows()`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		if _, err := linkqueries.QueryLinkMembers().CreateMany(t.Context(), tx, []linkqueries.MemberDraft{
			linkqueries.MemberDraft{}.SetName("skip2"), linkqueries.MemberDraft{}.SetName("skip1"), linkqueries.MemberDraft{}.SetName("keep"),
		}); err != nil {
			return err
		}
		source := linkqueries.QueryLinkMembers().OrderBy(linkqueries.MemberFields().Name.Desc())
		rows, err := archiveMembers(source).Values(archiveValues(t)).Returning(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "keep" {
			return errors.New("return bound silently truncated candidates suppressed by a database trigger")
		}
		return nil
	})
}

func TestPostgresInsertSelectCountDoesNotCollectOrCapSourceRows(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		drafts := make([]linkqueries.MemberDraft, query.MaxInsertRows)
		for i := range drafts {
			drafts[i] = linkqueries.MemberDraft{}.SetName("source")
		}
		if _, err := linkqueries.QueryLinkMembers().CreateMany(t.Context(), tx, drafts); err != nil {
			return err
		}
		if _, err := linkqueries.QueryLinkMembers().Create(t.Context(), tx, linkqueries.MemberDraft{}.SetName("last")); err != nil {
			return err
		}
		count, err := archiveMembers(linkqueries.QueryLinkMembers()).Values(archiveValues(t)).Exec(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != int64(query.MaxInsertRows+1) {
			return errors.New("count-only insert silently capped its source at the retained-model limit")
		}
		return nil
	})
}
