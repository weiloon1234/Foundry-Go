package auditqueries_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func prepareAccountPolicy(t *testing.T, policy orm.AuditPolicy[auditqueries.Account], modules ...foundation.Provider) consumer {
	t.Helper()
	return preparePolicies(t, policy, auditqueries.LabelPolicy, modules...)
}

func preparePolicies(t *testing.T, policy orm.AuditPolicy[auditqueries.Account], labels orm.AuditPolicy[auditqueries.Label], modules ...foundation.Provider) consumer {
	t.Helper()
	domain := foundation.Module{Name: "domain.audit", Requires: []foundation.ProviderID{"database"}, OnRegister: func(r *foundation.Registrar) error {
		return audit.Register(r, auditqueries.Pool, auditqueries.History, audit.Config{Area: "accounts", RetentionDays: 90}, orm.Auditing(auditqueries.Accounts.Table, policy), orm.Auditing(auditqueries.Labels.Table, labels))
	}}
	return prepareAuditDomain(t, true, domain, modules...)
}

func TestAutomaticAuditIgnoreIsOptInAndKeepsOtherHooks(t *testing.T) {
	for name, disclosure := range map[string]record.Disclosure{"exclude": record.Exclude, "ignore": record.Ignore} {
		t.Run(name, func(t *testing.T) { testAuditDisclosure(t, disclosure) })
	}
}

func testAuditDisclosure(t *testing.T, disclosure record.Disclosure) {
	t.Helper()
	rejected := errors.New("excluded update rejected by another observer")
	observer := foundation.Module{Name: "audit.empty-update-observer", Requires: []foundation.ProviderID{"database"}, OnRegister: func(r *foundation.Registrar) error {
		return orm.RegisterObserver(r, auditqueries.Pool, orm.NewObserver[auditqueries.Account]("empty-update-observer"), func(foundation.Resolver) (func() orm.Hooks[auditqueries.Account], error) {
			return func() orm.Hooks[auditqueries.Account] {
				return orm.Hooks[auditqueries.Account]{Updated: func(_ context.Context, _ *database.Tx, changes orm.Changes[auditqueries.Account]) error {
					after, _ := changes.After().Get()
					if note, ok := after.Note.Get(); ok && note == "observer-reject" {
						return rejected
					}
					return nil
				}}
			}, nil
		})
	}}
	policy := orm.AuditPolicyOf(auditqueries.Accounts.Table, orm.AuditField(orm.NullableTextExpr(auditqueries.Accounts.Note), disclosure))
	c := prepareAccountPolicy(t, policy, observer)
	ctx := t.Context()
	input := draft(t, "empty-update@example.test")
	var account auditqueries.Account
	if err := c.within(ctx, func(tx *database.Tx) error {
		var err error
		account, err = orm.Insert(ctx, tx, auditqueries.Accounts.Table, input...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	checkCount := func(t *testing.T, want int64) {
		t.Helper()
		if err := c.within(ctx, func(tx *database.Tx) error {
			page := history(t, c, ctx, tx, account)
			if page.Total != want {
				t.Errorf("audit count = %d, want %d", page.Total, want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	checkCount(t, 1)
	base := int64(1)
	if disclosure == record.Exclude {
		base++
	}
	for _, test := range []struct {
		name        string
		assignments []orm.Assignment[auditqueries.Account]
		want        int64
	}{
		{"private-only", []orm.Assignment[auditqueries.Account]{orm.NullableTextSet(auditqueries.Accounts.Note, "private-only")}, base},
		{"recorded-change", []orm.Assignment[auditqueries.Account]{orm.NullableTextSet(auditqueries.Accounts.Note, "recorded-change"), orm.InputTextSet(auditqueries.Accounts.Email, "changed@example.test")}, base + 1},
		{"recorded-unchanged", []orm.Assignment[auditqueries.Account]{orm.NullableTextSet(auditqueries.Accounts.Note, "recorded-unchanged"), orm.InputTextSet(auditqueries.Accounts.Email, " CHANGED@EXAMPLE.TEST ")}, base + 2},
		{"redacted", []orm.Assignment[auditqueries.Account]{orm.NullableTextSet(auditqueries.Accounts.Note, "redacted"), orm.TextSet(auditqueries.Accounts.PasswordHash, "changed-private-hash")}, base + 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := account
			if err := c.within(ctx, func(tx *database.Tx) error {
				var err error
				account, err = orm.Update(ctx, tx, auditqueries.Accounts.Query(), auditqueries.Accounts.Key(), account.ID, test.assignments...)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := c.within(ctx, func(tx *database.Tx) error {
				var err error
				account, err = orm.RequireKey(ctx, tx, auditqueries.Accounts.Table, account.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if note, ok := account.Note.Get(); !ok || note != test.name || account.UpdatedAt == before.UpdatedAt {
				t.Fatal("excluded field or managed update timestamp did not change")
			}
			checkCount(t, test.want)
		})
	}
	if err := c.within(ctx, func(tx *database.Tx) error {
		page := history(t, c, ctx, tx, account)
		var unchanged, redacted bool
		for _, row := range page.Items {
			note, err := orm.AuditReadNullable(row.Changes(), orm.NullableTextExpr(auditqueries.Accounts.Note))
			if err != nil || note.IsSet() {
				return errors.New("excluded or ignored field entered audit record")
			}
			payload, err := row.Changes().Entry().Payload()
			if err != nil || strings.Contains(payload, `"name":"note"`) || strings.Contains(payload, "private-note") || strings.Contains(payload, "private-only") {
				return errors.New("private note leaked into audit payload")
			}
			if row.Changes().Operation() != lifecycle.Update {
				continue
			}
			email, err := orm.AuditRead(row.Changes(), orm.InputTextExpr(auditqueries.Accounts.Email))
			if err != nil {
				return err
			}
			if field, ok := email.Get(); ok && field.Assigned() && !field.Changed() {
				unchanged = true
			}
			password, err := orm.AuditRead(row.Changes(), orm.TextExpr(auditqueries.Accounts.PasswordHash))
			if err != nil {
				return err
			}
			if field, ok := password.Get(); ok && field.After().State() == record.Redacted {
				redacted = true
			}
		}
		if !unchanged || !redacted {
			return errors.New("assigned unchanged or redacted field lost its audit record")
		}
		_, err := orm.Update(ctx, tx, auditqueries.Accounts.Query(), auditqueries.Accounts.Key(), account.ID, orm.NullableTextSet(auditqueries.Accounts.Note, "observer-reject"))
		if !errors.Is(err, rejected) {
			return errors.New("empty audit update skipped another observer")
		}
		// The existing local Saved hook rejects this stored email. Set it directly
		// so the following ORM write assigns only an excluded field and timestamps.
		if _, err := tx.Exec(ctx, `UPDATE audited_accounts SET email = 'reject@example.test'`); err != nil {
			return err
		}
		_, err = orm.Update(ctx, tx, auditqueries.Accounts.Query(), auditqueries.Accounts.Key(), account.ID, orm.NullableTextSet(auditqueries.Accounts.Note, "local-hook-reject"))
		if !errors.Is(err, auditqueries.ErrRejected) {
			return errors.New("empty audit update skipped local Saved hook")
		}
		stored, err := orm.Require(ctx, tx, auditqueries.Accounts.Query())
		if err != nil {
			return err
		}
		if note, _ := stored.Note.Get(); note != "redacted" {
			return errors.New("rejected excluded update persisted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	checkCount(t, base+3)
}

func TestAutomaticAuditRetainsCreatedAtAndTimestampOnlyUpdates(t *testing.T) {
	f := auditqueries.Accounts
	policy := orm.AuditPolicyOf(f.Table, orm.AuditField(orm.NullableTextExpr(f.Note), record.Ignore), orm.AuditField(orm.TimeExpr(f.UpdatedAt), record.Ignore))
	c := prepareAccountPolicy(t, policy)
	ctx := t.Context()
	old, err := temporal.NewDateTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	input := draft(t, "backdated@example.test")
	if err := c.within(ctx, func(tx *database.Tx) error {
		account, err := orm.Insert(ctx, tx, f.Table, input...)
		if err != nil {
			return err
		}
		account, err = orm.Update(ctx, tx, f.Query(), f.Key(), account.ID, orm.TimeSet(f.CreatedAt, old), orm.NullableTextSet(f.Note, "ignored-backdate"))
		if err != nil {
			return err
		}
		if account.CreatedAt != old {
			return errors.New("created_at was not backdated")
		}
		page := history(t, c, ctx, tx, account)
		if page.Total != 2 || len(page.Items) != 2 {
			return errors.New("backdating created_at lost history")
		}
		field, err := orm.AuditRead(page.Items[0].Changes(), orm.TimeExpr(f.CreatedAt))
		if err != nil {
			return err
		}
		change, ok := field.Get()
		if !ok || !change.Changed() {
			return errors.New("created_at change omitted")
		}
		stored, err := change.After().Get()
		if got, ok := stored.Get(); err != nil || !ok || got != old {
			return errors.New("backdated audit value differs from stored value")
		}
		// The managed updated_at is Ignore too: it alone must not opt a write out.
		if _, err := orm.Update(ctx, tx, f.Query(), f.Key(), account.ID, orm.TimeSet(f.UpdatedAt, old)); err != nil {
			return err
		}
		if history(t, c, ctx, tx, account).Total != 3 {
			return errors.New("timestamp-only update lost history")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticAuditAllIgnoredFieldsStillRecordLifecycle(t *testing.T) {
	f := auditqueries.Accounts
	policy := orm.AuditPolicyOf(f.Table,
		orm.AuditField(orm.InputTextExpr(f.Email), record.Ignore),
		orm.AuditField(orm.TextExpr(f.PasswordHash), record.Ignore),
		orm.AuditField(orm.NullableTextExpr(f.Note), record.Ignore),
		orm.AuditField(orm.NumberExpr(f.Balance), record.Ignore),
		orm.AuditField(orm.JSONExpr(f.Preferences), record.Ignore),
		orm.AuditField(orm.TimeExpr(f.CreatedAt), record.Ignore),
		orm.AuditField(orm.TimeExpr(f.UpdatedAt), record.Ignore),
		orm.AuditField(orm.NullableTimeExpr(f.DeletedAt), record.Ignore))
	labels := auditqueries.Labels
	c := preparePolicies(t, policy, orm.AuditPolicyOf(labels.Table, orm.AuditField(orm.TextExpr(labels.Name), record.Ignore)))
	ctx := t.Context()
	input := draft(t, "all-ignored@example.test")
	if err := c.within(ctx, func(tx *database.Tx) error {
		account, err := orm.Insert(ctx, tx, f.Table, input...)
		if err != nil {
			return err
		}
		check := func(want int64, operation lifecycle.Operation) error {
			page := history(t, c, ctx, tx, account)
			if page.Total != want || len(page.Items) != int(want) || page.Items[0].Changes().Operation() != operation {
				return errors.New("all-Ignore lifecycle operation lost history")
			}
			payload, err := page.Items[0].Changes().Entry().Payload()
			if err != nil {
				return err
			}
			if strings.Count(payload, `"name":`) != 1 || !strings.Contains(payload, `"name":"id"`) {
				return errors.New("all-Ignore record retained a non-primary field")
			}
			return nil
		}
		if err := check(1, lifecycle.Create); err != nil {
			return err
		}
		if _, err := orm.Update(ctx, tx, f.Query(), f.Key(), account.ID, orm.NullableTextSet(f.Note, "ignored")); err != nil {
			return err
		}
		if err := check(1, lifecycle.Create); err != nil {
			return err
		}
		// Soft delete and restore touch only ignored deleted_at and managed updated_at.
		// Removing the update-only guard must therefore fail these row counts.
		if _, err := orm.Delete(ctx, tx, f.Query(), f.Key(), account.ID); err != nil {
			return err
		}
		if err := check(2, lifecycle.SoftDelete); err != nil {
			return err
		}
		if _, err := orm.Restore(ctx, tx, f.Table, account.ID); err != nil {
			return err
		}
		if err := check(3, lifecycle.Restore); err != nil {
			return err
		}
		if _, err := orm.ForceDelete(ctx, tx, f.Query(), f.Soft, f.Key(), account.ID); err != nil {
			return err
		}
		if err := check(4, lifecycle.ForceDelete); err != nil {
			return err
		}
		label, err := orm.Insert(ctx, tx, labels.Table, orm.KeySet(labels.Key(), "all-ignored"), orm.TextSet(labels.Name, "ignored-label"))
		if err != nil {
			return err
		}
		if _, err := orm.Delete(ctx, tx, labels.Query(), labels.Key(), label.Code); err != nil {
			return err
		}
		page, err := audit.ModelHistory(ctx, tx, c.recorder, auditqueries.LabelIdentity.ReferenceOf(label), orm.PageRequest{Number: 1, Size: 10})
		if err != nil {
			return err
		}
		if page.Total != 2 || len(page.Items) != 2 || page.Items[0].Changes().Operation() != lifecycle.Delete || page.Items[1].Changes().Operation() != lifecycle.Create {
			return errors.New("all-Ignore physical lifecycle lost history")
		}
		for _, row := range page.Items {
			name, err := orm.AuditRead(row.Changes(), orm.TextExpr(labels.Name))
			if err != nil || name.IsSet() {
				return errors.New("physical lifecycle disclosed ignored name")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
