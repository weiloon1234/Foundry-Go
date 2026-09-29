package auditqueries_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type consumer struct {
	recorder *audit.Recorder
	within   func(context.Context, func(*database.Tx) error) error
}

func prepare(t *testing.T, withStorage bool) consumer {
	t.Helper()
	app := testkit.Start(t, foundry.New().Register(auditqueries.Domain(), postgres.Module("database", auditqueries.Pool, pgtest.Config(t))))
	db, err := foundation.Resolve(app.Services(), auditqueries.Pool)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := foundation.Resolve(app.Services(), auditqueries.History)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	within := func(ctx context.Context, fn func(*database.Tx) error) error {
		return db.Transaction(ctx, func(tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, statement := range []string{
			`CREATE TABLE audited_accounts (id uuid PRIMARY KEY,email text NOT NULL UNIQUE,password_hash text NOT NULL,note text,balance numeric NOT NULL,preferences jsonb NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`,
			`CREATE TABLE audited_labels (code text PRIMARY KEY,name text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), statement); err != nil {
				return err
			}
		}
		if withStorage {
			for _, definition := range audit.Migrations() {
				for _, statement := range definition.SQL {
					if _, err := tx.Exec(t.Context(), statement); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return consumer{recorder: recorder, within: within}
}

func draft(t *testing.T, email string) auditqueries.AccountDraft {
	t.Helper()
	prefs, err := value.NewJSON(auditqueries.Preferences{Theme: "dark", APIToken: "private-nested-token"})
	if err != nil {
		t.Fatal(err)
	}
	balance, err := decimal.Parse("9007199254740993.001200")
	if err != nil {
		t.Fatal(err)
	}
	return auditqueries.AccountDraft{}.SetEmail(email).SetPasswordHash("private-password-hash").SetNote("private-note").SetBalance(balance).SetPreferences(prefs)
}

func history(t *testing.T, c consumer, ctx context.Context, tx *database.Tx, account auditqueries.Account) query.Page[audit.ModelRecord[auditqueries.Account, model.ID[auditqueries.Account]]] {
	t.Helper()
	page, err := audit.ModelHistory(ctx, tx, c.recorder, account.FoundryReference(), query.PageRequest{Number: 1, Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestAutomaticAuditUsesStoredChangesAndConcreteFields(t *testing.T) {
	c := prepare(t, true)
	origin, err := (attribution.Origin{}).WithSystem("consumer.audit.writer")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	input := draft(t, "  MEMBER@EXAMPLE.TEST ")
	var account auditqueries.Account
	if err := c.within(ctx, func(tx *database.Tx) error {
		var err error
		account, err = auditqueries.QueryAuditedAccounts().Create(ctx, tx, input)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.within(ctx, func(tx *database.Tx) error {
		page := history(t, c, ctx, tx, account)
		if page.Total != 1 || len(page.Items) != 1 {
			return errors.New("create audit not recorded exactly once")
		}
		row := page.Items[0]
		loaded, err := audit.FindModel(ctx, tx, c.recorder, (auditqueries.Account{}).FoundryReference(), row.ID())
		if err != nil || !loaded.IsSet() {
			return errors.New("typed model audit reload failed")
		}
		if row.Origin() != origin || row.Area() != "accounts" || row.Changes().Operation() != lifecycle.Create {
			return errors.New("audit lost operation/origin")
		}
		fields, err := auditqueries.AccountAuditFields(row.Changes())
		if err != nil {
			return err
		}
		email, present := fields.Email.Get()
		if !present || email.Before().State() != record.Absent {
			return errors.New("create snapshot fabricated previous model")
		}
		snapshot, err := email.After().Get()
		if err != nil {
			return err
		}
		stored, present := snapshot.Get()
		if !present || stored != "member@example.test" {
			return errors.New("audit used getter or pre-mutator input")
		}
		password, present := fields.PasswordHash.Get()
		if !present || password.After().State() != record.Redacted {
			return errors.New("credential not redacted")
		}
		if fields.Note.IsSet() {
			return errors.New("excluded field remained")
		}
		balance, present := fields.Balance.Get()
		if !present {
			return errors.New("balance omitted")
		}
		amount, err := balance.After().Get()
		if err != nil {
			return err
		}
		exact, present := amount.Get()
		if !present || exact != account.Balance {
			return errors.New("audit lost exact decimal")
		}
		preferences, present := fields.Preferences.Get()
		if !present || preferences.After().State() != record.RedactedJSON {
			return errors.New("nested JSON not redacted")
		}
		if _, err := preferences.After().Get(); !errors.Is(err, fault.Missing) {
			return errors.New("partially redacted preferences hydrated")
		}
		payload, err := row.Changes().Entry().Payload()
		if err != nil {
			return err
		}
		for _, private := range []string{"private-password-hash", "private-note", "private-nested-token", "display:"} {
			if strings.Contains(payload, private) {
				return errors.New("audit captured private/presentation data")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.within(ctx, func(tx *database.Tx) error {
		var err error
		account, err = auditqueries.QueryAuditedAccounts().Update(ctx, tx, account.ID, auditqueries.AccountDraft{}.SetEmail(" MEMBER@EXAMPLE.TEST ").ClearNote())
		if err != nil {
			return err
		}
		page := history(t, c, ctx, tx, account)
		if page.Total != 2 {
			return errors.New("update audit missing")
		}
		for _, row := range page.Items {
			if row.Changes().Operation() == lifecycle.Update {
				fields, err := auditqueries.AccountAuditFields(row.Changes())
				if err != nil {
					return err
				}
				email, present := fields.Email.Get()
				if !present || !email.Assigned() || email.Changed() {
					return errors.New("assignment/dirty tracking changed during audit")
				}
				// Updates keep only touched fields; the untouched balance is omitted.
				if fields.Balance.IsSet() || fields.Preferences.IsSet() {
					return errors.New("update audit stored untouched fields")
				}
				timeline, err := audit.ModelTimeline(ctx, tx, c.recorder, account.FoundryReference(), audit.TimelineRequest{Size: 1})
				if err != nil {
					return err
				}
				if len(timeline.Items) != 1 || timeline.Items[0].ID() != row.ID() || !timeline.Next.IsSet() {
					return errors.New("keyset history did not start with the latest update")
				}
				return nil
			}
		}
		return errors.New("update operation omitted")
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticAuditDeletionRestorationAndNaturalKeys(t *testing.T) {
	c := prepare(t, true)
	ctx := t.Context()
	input := draft(t, "lifecycle@example.test")
	if err := c.within(ctx, func(tx *database.Tx) error {
		account, err := auditqueries.QueryAuditedAccounts().Create(ctx, tx, input)
		if err != nil {
			return err
		}
		if _, err := auditqueries.QueryAuditedAccounts().Delete(ctx, tx, account.ID); err != nil {
			return err
		}
		if _, err := auditqueries.QueryAuditedAccounts().Restore(ctx, tx, account.ID); err != nil {
			return err
		}
		if _, err := auditqueries.QueryAuditedAccounts().ForceDelete(ctx, tx, account.ID); err != nil {
			return err
		}
		page := history(t, c, ctx, tx, account)
		counts := map[lifecycle.Operation]int{}
		for _, row := range page.Items {
			counts[row.Changes().Operation()]++
		}
		if page.Total != 4 || counts[lifecycle.Create] != 1 || counts[lifecycle.SoftDelete] != 1 || counts[lifecycle.Restore] != 1 || counts[lifecycle.ForceDelete] != 1 {
			return errors.New("special deletion audit duplicated or omitted")
		}
		label, err := auditqueries.QueryAuditedLabels().Create(ctx, tx, auditqueries.LabelDraft{}.SetCode("natural-key").SetName("created"))
		if err != nil {
			return err
		}
		if _, err := auditqueries.QueryAuditedLabels().Delete(ctx, tx, label.Code); err != nil {
			return err
		}
		labels, err := audit.ModelHistory(ctx, tx, c.recorder, label.FoundryReference(), query.PageRequest{Number: 1, Size: 10})
		if err != nil {
			return err
		}
		if labels.Total != 2 {
			return errors.New("natural key audit missing")
		}
		for _, row := range labels.Items {
			var reference model.Reference[auditqueries.Label, auditqueries.LabelCode]
			reference, err = row.Changes().Subject()
			if err != nil || reference.Key() != label.Code {
				return errors.New("natural key lost its stored type")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestModelAndAuditRollbackTogetherAndMissingStorageFails(t *testing.T) {
	for _, storage := range []bool{true, false} {
		t.Run(map[bool]string{true: "later-hook-failure", false: "missing-storage"}[storage], func(t *testing.T) {
			c := prepare(t, storage)
			ctx := t.Context()
			input := draft(t, "reject@example.test")
			if err := c.within(ctx, func(tx *database.Tx) error {
				_, err := auditqueries.QueryAuditedAccounts().Create(ctx, tx, input)
				if err == nil {
					return errors.New("failed audit/hook write succeeded")
				}
				if storage && !errors.Is(err, auditqueries.ErrRejected) {
					return err
				}
				count, err := auditqueries.QueryAuditedAccounts().Count(ctx, tx)
				if err != nil {
					return err
				}
				if count != 0 {
					return errors.New("failed write retained model")
				}
				if storage {
					rows, err := tx.Query(ctx, `SELECT count(*) FROM foundry_audit`)
					if err != nil {
						return err
					}
					if !rows.Next() {
						return errors.Join(errors.New("missing audit count"), rows.Err(), rows.Close())
					}
					if err := rows.Scan(&count); err != nil {
						return err
					}
					if err := rows.Close(); err != nil {
						return err
					}
					if count != 0 {
						return errors.New("later hook failure retained audit")
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	c := prepare(t, true)
	ctx := t.Context()
	input := draft(t, "rollback@example.test")
	rollback := errors.New("outer rollback")
	var account auditqueries.Account
	if err := c.within(ctx, func(tx *database.Tx) error {
		var err error
		account, err = auditqueries.QueryAuditedAccounts().Create(ctx, tx, input)
		if err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := c.within(ctx, func(tx *database.Tx) error {
		page := history(t, c, ctx, tx, account)
		if page.Total != 0 {
			return errors.New("outer rollback retained audit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBulkAuditSemanticsAndPerModelAlternative(t *testing.T) {
	c := prepare(t, true)
	ctx := t.Context()
	if err := c.within(ctx, func(tx *database.Tx) error {
		label, err := auditqueries.QueryAuditedLabels().Create(ctx, tx, auditqueries.LabelDraft{}.SetCode("bulk").SetName("old"))
		if err != nil {
			return err
		}
		f := auditqueries.LabelFields()
		q := auditqueries.QueryAuditedLabels().Where(f.Code.Eq(label.Code))
		if _, err := auditqueries.UpdateLabelFrom(q, q).MatchCode(f.Code.Value()).Values(auditqueries.LabelDraft{}.SetName("bulk")).Exec(ctx, tx); err != nil {
			return err
		}
		page, err := audit.ModelHistory(ctx, tx, c.recorder, label.FoundryReference(), query.PageRequest{Number: 1, Size: 10})
		if err != nil {
			return err
		}
		if page.Total != 1 {
			return errors.New("set-based write dispatched per-model audit")
		}
		if _, err := q.UpdateEach(ctx, tx, 1, func(context.Context, *database.Tx, auditqueries.Label) (auditqueries.LabelDraft, error) {
			return auditqueries.LabelDraft{}.SetName("per-row"), nil
		}); err != nil {
			return err
		}
		page, err = audit.ModelHistory(ctx, tx, c.recorder, label.FoundryReference(), query.PageRequest{Number: 1, Size: 10})
		if err != nil {
			return err
		}
		if page.Total != 2 {
			return errors.New("per-row write omitted audit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
