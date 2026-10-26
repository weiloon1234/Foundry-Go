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
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	sqlrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func archiveValues(t *testing.T) []orm.Assignment[linkqueries.Archive] {
	t.Helper()
	amount, err := decimal.Parse("12345678901234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := value.NewJSON([]string{"first", "quoted '"})
	if err != nil {
		t.Fatal(err)
	}
	return []orm.Assignment[linkqueries.Archive]{linkqueries.Archives.Tag.Set(linkqueries.ArchiveTagInput{Text: " BATCH "}), linkqueries.Archives.Amount.Set(amount), linkqueries.Archives.Payload.Set(payload)}
}

func archiveMembers(source orm.Query[linkqueries.Member]) orm.InsertSelect[linkqueries.Member, linkqueries.Archive] {
	fields := linkqueries.Members
	return sqlrows.InsertFrom(sqlrows.From(source), linkqueries.Archives.Query(), sqlrows.MapInsert(linkqueries.Archives.MemberID, expr.Value(fields.ID)), sqlrows.MapInsert(linkqueries.Archives.Name, expr.Value(fields.Name)), sqlrows.MapInsert(linkqueries.Archives.Alias, expr.Value(fields.Alias)))
}

func TestPostgresInsertSelectPreservesTypesDefaultsAndBulkSemantics(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		members, err := linkqueries.Members.InsertMany(t.Context(), tx, [][]orm.Assignment[linkqueries.Member]{
			[]orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("alpha")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("beta"), linkqueries.Members.Alias.Set("Bee")},
		})
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		old := clock.Now().Add(-24 * time.Hour).UTC().Truncate(time.Microsecond)
		draft := append(archiveValues(t), linkqueries.Archives.CreatedAt.Set(old))
		inserted, err := archiveMembers(linkqueries.Members.Query()).Values(draft...).Returning(ctx, wrappedTransactor{tx}, 2)
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
		amount, err := decimal.Parse("12345678901234567890.123456789")
		if err != nil {
			return err
		}
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
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if text := fmt.Sprintf(format, archiveMembers(linkqueries.Members.Query()).Values(draft...)); text != "orm insert from query" {
				return errors.New("generated insertion diagnostics exposed captured inputs")
			}
		}
		// Reuse the actual assignments: hooks must see the original input values
		// and omissions, before mutators, identity synthesis and timestamps.
		inspected := false
		trace.InspectArchive = func(w *orm.Write[linkqueries.Archive]) error {
			fields := linkqueries.Archives
			tag, tagOK := orm.WriteGet(w, fields.Tag.Set).Get()
			a, amountOK := orm.WriteGet(w, fields.Amount.Set).Get()
			payload, payloadOK := orm.WriteGet(w, fields.Payload.Set).Get()
			decoded, err := payload.Decode()
			created, createdOK := orm.WriteGet(w, fields.CreatedAt.Set).Get()
			if !tagOK || tag.Text != " BATCH " || !amountOK || a != amount || !payloadOK || err != nil || !slices.Equal(decoded, []string{"first", "quoted '"}) || !createdOK || created != old || orm.WriteIsSet(w, fields.Key().Set) || orm.WriteIsSet(w, fields.UpdatedAt.Set) {
				return errors.New("insert preparation changed the source draft values or omissions")
			}
			inspected = true
			return nil
		}
		ordinary := append(slices.Clone(draft), linkqueries.Archives.MemberID.Set(members[0].ID), linkqueries.Archives.Name.Set("ordinary"))
		if _, err := linkqueries.Archives.Insert(ctx, tx, ordinary...); !errors.Is(err, linkqueries.ErrVeto) {
			return errors.New("fixture did not exercise real creation hooks")
		}
		if !inspected {
			return errors.New("reused assignments were not inspected")
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
			runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				if _, err := linkqueries.Members.InsertMany(t.Context(), tx, [][]orm.Assignment[linkqueries.Member]{[]orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("one")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("two")}}); err != nil {
					return err
				}
				draft := archiveValues(t)
				plan := archiveMembers(linkqueries.Members.Query()).Values(draft...)
				switch mode {
				case "return_bound":
					rows, err := plan.Returning(t.Context(), tx, 1)
					if !errors.Is(err, database.TooManyRows) || rows != nil {
						return errors.New("excess insertion results did not fail atomically")
					}
				case "mutator_veto":
					count, err := plan.Values(linkqueries.Archives.Tag.Set(linkqueries.ArchiveTagInput{Text: "reject"}), draft[1], draft[2]).Exec(t.Context(), tx)
					if !errors.Is(err, linkqueries.ErrVeto) || count != 0 {
						return errors.New("literal setter veto was bypassed")
					}
				case "unique_violation":
					id, err := model.NewIDAt[linkqueries.Archive](time.Now())
					if err != nil {
						return err
					}
					count, err := plan.Values(append(slices.Clone(draft), linkqueries.Archives.Key().Set(id))...).Exec(t.Context(), tx)
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
				count, err := linkqueries.Archives.Query().Count(t.Context(), tx)
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
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		members, err := linkqueries.Members.InsertMany(t.Context(), tx, [][]orm.Assignment[linkqueries.Member]{
			[]orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("alpha")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("beta")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("gamma")},
		})
		if err != nil {
			return err
		}
		fields := linkqueries.Members
		alpha := slices.IndexFunc(members, func(member linkqueries.Member) bool { return member.Name == "alpha" })
		if alpha < 0 {
			return errors.New("fixture did not return its alpha member")
		}
		if _, err := linkqueries.Members.Delete(t.Context(), tx, members[alpha].ID); err != nil {
			return err
		}
		source := linkqueries.Members.Query().OrderBy(fields.Name.Asc()).Offset(1).Limit(1)
		inserted, err := archiveMembers(source).Values(archiveValues(t)...).Returning(t.Context(), tx, 2)
		if err != nil || len(inserted) != 1 || inserted[0].Name != "gamma" {
			return errors.New("source soft visibility or ordered window changed")
		}
		cte := sqlrows.CTE("archivable", sqlrows.ModelRecord(linkqueries.Members.Query().OnlyTrashed()))
		aliased := sqlrows.AsRecord[archiveSourceAlias](cte, "archived_member")

		count, err := sqlrows.InsertFrom(aliased, linkqueries.Archives.Query(), sqlrows.MapInsert(linkqueries.Archives.MemberID, expr.At(sqlrows.Scope(aliased), linkqueries.Members.ID)), sqlrows.MapInsert(linkqueries.Archives.Name, expr.At(sqlrows.Scope(aliased), linkqueries.Members.Name)), sqlrows.MapInsert(linkqueries.Archives.Alias, expr.At(sqlrows.Scope(aliased), linkqueries.Members.Alias))).Values(archiveValues(t)...).Exec(t.Context(), tx)
		if err != nil || count != 1 {
			return errors.New("insert selection lost aliased CTE or explicit deleted scope")
		}
		empty := archiveMembers(linkqueries.Members.Query().Where(fields.Name.Eq("absent"))).Values(archiveValues(t)...)
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
		if _, err := archiveMembers(linkqueries.Members.Query()).Values(archiveValues(t)...).Exec(ctx, tx); !errors.Is(err, context.Canceled) {
			return errors.New("canceled insertion started work")
		}
		return nil
	})
}

func TestPostgresInsertSelectReturningDoesNotTruncateSuppressedSourceRows(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		for _, ddl := range []string{
			`CREATE FUNCTION skip_archive_rows() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name LIKE 'skip%' THEN RETURN NULL; END IF; RETURN NEW; END $$`,
			`CREATE TRIGGER skip_archive_rows BEFORE INSERT ON link_archives FOR EACH ROW EXECUTE FUNCTION skip_archive_rows()`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		if _, err := linkqueries.Members.InsertMany(t.Context(), tx, [][]orm.Assignment[linkqueries.Member]{
			[]orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("skip2")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("skip1")}, []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("keep")},
		}); err != nil {
			return err
		}
		source := linkqueries.Members.Query().OrderBy(linkqueries.Members.Name.Desc())
		rows, err := archiveMembers(source).Values(archiveValues(t)...).Returning(t.Context(), tx, 1)
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
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		drafts := make([][]orm.Assignment[linkqueries.Member], orm.MaxPerModelWriteRows)
		for i := range drafts {
			drafts[i] = []orm.Assignment[linkqueries.Member]{linkqueries.Members.Name.Set("source")}
		}
		if _, err := linkqueries.Members.InsertMany(t.Context(), tx, drafts); err != nil {
			return err
		}
		if _, err := linkqueries.Members.Insert(t.Context(), tx, linkqueries.Members.Name.Set("last")); err != nil {
			return err
		}
		count, err := archiveMembers(linkqueries.Members.Query()).Values(archiveValues(t)...).Exec(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != int64(orm.MaxPerModelWriteRows+1) {
			return errors.New("count-only insert silently capped its source at the retained-model limit")
		}
		return nil
	})
}
