package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type approved struct {
	Labels []string `json:"labels"`
}
type subject struct{}

func auditTransactions(t *testing.T) func(context.Context, func(*database.Tx) error) error {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	within := func(ctx context.Context, fn func(*database.Tx) error) error {
		return db.Transaction(ctx, func(tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	if _, err := migrate.New(audit.Migrations()...); err != nil {
		t.Fatal(err)
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, definition := range audit.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return within
}

func newRecorder(t *testing.T) *audit.Recorder {
	t.Helper()
	recorder, err := audit.New(audit.Config{Area: "test.audit", RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

func TestDomainAuditCommitSavepointRollbackAndTypedReload(t *testing.T) {
	within, recorder := auditTransactions(t), newRecorder(t)
	action := audit.Define[approved]("record.approved", 1)
	origin, err := (attribution.Origin{}).WithSystem("test.approver")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.NewReference[subject]("subjects", int64(7), codec.Signed[int64]())
	var kept, discarded audit.ActionID[approved]
	rejected := errors.New("rollback audit savepoint")
	input := approved{Labels: []string{"frozen"}}
	if err := within(ctx, func(tx *database.Tx) error {
		var err error
		kept, err = audit.RecordFor(ctx, tx, recorder, action, ref, input)
		if err != nil {
			return err
		}
		input.Labels[0] = "mutated"
		err = tx.Transaction(ctx, func(child *database.Tx) error {
			discarded, err = action.Record(ctx, child, recorder, approved{Labels: []string{"discarded"}})
			if err != nil {
				return err
			}
			return rejected
		})
		if !errors.Is(err, rejected) {
			return errors.New("savepoint rollback lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		result, err := action.Find(ctx, tx, recorder, kept)
		if err != nil {
			return err
		}
		row, present := result.Get()
		if !present || row.ID() != kept || row.Origin() != origin || row.CreatedAt().IsZero() {
			return errors.New("audit lost committed metadata")
		}
		identity, present := row.Subject().Get()
		if !present {
			return errors.New("audit omitted subject")
		}
		restored, err := ref.Parse(identity)
		if err != nil || restored.Key() != 7 {
			return errors.New("audit changed concrete subject")
		}
		first, err := row.Document().Decode()
		if err != nil || first.Labels[0] != "frozen" {
			return errors.New("audit did not capture DTO")
		}
		first.Labels[0] = "changed"
		second, err := row.Document().Decode()
		if err != nil || second.Labels[0] != "frozen" {
			return errors.New("audit decode aliases history")
		}
		missing, err := action.Find(ctx, tx, recorder, discarded)
		if err != nil {
			return err
		}
		if missing.IsSet() {
			return errors.New("rolled back audit survived")
		}
		foreign, err := audit.Define[approved]("record.approved", 2).Find(ctx, tx, recorder, kept)
		if err != nil {
			return err
		}
		if foreign.IsSet() {
			return errors.New("foreign version selected")
		}
		other, err := audit.WithArea(ctx, "other.area")
		if err != nil {
			return err
		}
		foreign, err = action.Find(other, tx, recorder, kept)
		if err != nil {
			return err
		}
		if foreign.IsSet() {
			return errors.New("foreign area selected")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var outer audit.ActionID[approved]
	if err := within(ctx, func(tx *database.Tx) error {
		var err error
		outer, err = action.Record(ctx, tx, recorder, approved{Labels: []string{}})
		if err != nil {
			return err
		}
		return rejected
	}); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		result, err := action.Find(ctx, tx, recorder, outer)
		if err != nil {
			return err
		}
		if result.IsSet() {
			return errors.New("outer rollback retained audit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDomainAuditRedactionAndStoredCorruption(t *testing.T) {
	within, recorder := auditTransactions(t), newRecorder(t)
	type payload struct {
		AccessToken string `json:"accessToken"`
		Name        string `json:"name"`
	}
	action := audit.Define[payload]("credential.changed", 1)
	if err := within(t.Context(), func(tx *database.Tx) error {
		id, err := action.Record(t.Context(), tx, recorder, payload{AccessToken: "private-token", Name: "visible"})
		if err != nil {
			return err
		}
		result, err := action.Find(t.Context(), tx, recorder, id)
		if err != nil {
			return err
		}
		row, present := result.Get()
		if !present {
			return errors.New("audit missing")
		}
		text, err := row.Document().Payload()
		if err != nil || strings.Contains(text, "private-token") {
			return errors.New("audit leaked secret")
		}
		if _, err := row.Document().Decode(); !errors.Is(err, fault.Missing) {
			return errors.New("audit fabricated redacted DTO")
		}
		// Explicit corruption is confined to this retained integration schema.
		if _, err := tx.Exec(t.Context(), `UPDATE foundry_audit SET payload = $1::jsonb WHERE id=$2`, `{"accessToken":"secret-injected","name":"visible"}`, id.String()); err != nil {
			return err
		}
		if _, err := action.Find(t.Context(), tx, recorder, id); err == nil {
			return errors.New("corrupted redaction accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditRetentionIsBoundedScopedAndTransactional(t *testing.T) {
	within, recorder := auditTransactions(t), newRecorder(t)
	action := audit.Define[approved]("record.approved", 1)
	ctx := t.Context()
	var protected audit.ActionID[approved]
	if err := within(ctx, func(tx *database.Tx) error {
		for i := 0; i < 3; i++ {
			if _, err := action.Record(ctx, tx, recorder, approved{Labels: []string{}}); err != nil {
				return err
			}
		}
		other, err := audit.WithArea(ctx, "retained.area")
		if err != nil {
			return err
		}
		protected, err = action.Record(other, tx, recorder, approved{Labels: []string{}})
		if err != nil {
			return err
		}
		// Fixture age setup only, in this owned schema; no system clock mutation.
		_, err = tx.Exec(ctx, `UPDATE foundry_audit SET created_at = '2000-01-01T00:00:00Z'::timestamptz`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now, err := temporal.NewDateTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback retention")
	if err := within(ctx, func(tx *database.Tx) error {
		count, err := recorder.PruneRetention(ctx, tx, now, 1)
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("prune ignored row bound")
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		for _, expected := range []int64{2, 1, 0} {
			count, err := recorder.PruneRetention(ctx, tx, now, 2)
			if err != nil {
				return err
			}
			if count != expected {
				return errors.New("bounded prune/rollback count incorrect")
			}
		}
		other, err := audit.WithArea(ctx, "retained.area")
		if err != nil {
			return err
		}
		retained, err := action.Find(other, tx, recorder, protected)
		if err != nil {
			return err
		}
		if !retained.IsSet() {
			return errors.New("retention crossed area boundary")
		}
		if _, err := recorder.PruneBefore(ctx, tx, now, 0); !errors.Is(err, fault.Invalid) {
			return errors.New("unbounded prune accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
