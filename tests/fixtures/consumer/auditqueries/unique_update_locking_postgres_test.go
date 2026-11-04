package auditqueries_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestAuditedUniqueUpdateCanTakeForUpdateBeforeReading(t *testing.T) {
	c := prepare(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	f := auditqueries.Accounts
	input := draft(t, "unique-before@example.test")
	var account auditqueries.Account
	if err := c.within(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE account_children (id integer PRIMARY KEY, account_id uuid NOT NULL REFERENCES audited_accounts(id))`); err != nil {
			return err
		}
		var err error
		account, err = orm.Insert(ctx, tx, f.Table, input...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The child writer holds KEY SHARE but has not yet updated the parent.
	childReady, proceed, childDone := make(chan int, 1), make(chan struct{}), make(chan struct{})
	var childErr error
	go func() {
		defer close(childDone)
		childErr = c.within(ctx, func(tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO account_children (id, account_id) VALUES (1, $1)`, account.ID.String()); err != nil {
				return err
			}
			pid, err := auditBackendPID(ctx, tx)
			if err != nil {
				return err
			}
			childReady <- pid
			select {
			case <-proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err = orm.Update(ctx, tx, f.Query(), f.Key(), account.ID, orm.TextSet(f.PasswordHash, "child-writer"))
			return err
		})
	}()
	defer func() { cancel(); <-childDone }()
	var childPID int
	select {
	case childPID = <-childReady:
	case <-childDone:
		t.Fatal("child insert failed", childErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	uniqueReady, uniqueDone := make(chan int, 1), make(chan struct{})
	var uniqueErr error
	go func() {
		defer close(uniqueDone)
		uniqueErr = c.within(ctx, func(tx *database.Tx) error {
			pid, err := auditBackendPID(ctx, tx)
			if err != nil {
				return err
			}
			uniqueReady <- pid
			// orm.Lock is this API's explicit FOR UPDATE. Acquire it before the
			// hooked before-image read can take NO KEY UPDATE and later upgrade.
			if _, err := orm.RequireLocked(ctx, tx, orm.Lock(f.Query().Where(orm.KeyEq(f.Key(), account.ID)))); err != nil {
				return err
			}
			_, err = orm.Update(ctx, tx, f.Query(), f.Key(), account.ID, orm.InputTextSet(f.Email, "unique-after@example.test"))
			return err
		})
	}()
	defer func() { cancel(); <-uniqueDone }()
	var uniquePID int
	select {
	case uniquePID = <-uniqueReady:
	case <-uniqueDone:
		t.Fatal("unique writer failed before readiness", uniqueErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := waitAuditBlocker(ctx, c, childPID, uniquePID, uniqueDone); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	for _, done := range []<-chan struct{}{childDone, uniqueDone} {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if childErr != nil || uniqueErr != nil {
		t.Fatal("explicit FOR UPDATE did not avoid the upgrade deadlock", childErr, uniqueErr)
	}
	if err := c.within(ctx, func(tx *database.Tx) error {
		stored, err := orm.RequireKey(ctx, tx, f.Table, account.ID)
		if err != nil {
			return err
		}
		if stored.Email != "unique-after@example.test" || stored.PasswordHash != "child-writer" {
			return errors.New("competing parent writes did not both persist")
		}
		page := history(t, c, ctx, tx, account)
		if page.Total != 3 || len(page.Items) != 3 {
			return errors.New("competing parent writes lost history")
		}
		field, err := orm.AuditRead(page.Items[0].Changes(), orm.InputTextExpr(f.Email))
		if err != nil {
			return err
		}
		change, ok := field.Get()
		if !ok {
			return errors.New("unique update omitted its audit field")
		}
		before, err := change.Before().Get()
		if v, ok := before.Get(); err != nil || !ok || v != account.Email {
			return errors.New("unique update lost its original email")
		}
		rows, err := tx.Query(ctx, `SELECT count(*) FROM account_children WHERE account_id = $1`, account.ID.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return errors.Join(errors.New("missing child count"), rows.Err())
		}
		var count int64
		if err := rows.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("child insert did not persist")
		}
		return rows.Close()
	}); err != nil {
		t.Fatal(err)
	}
}
