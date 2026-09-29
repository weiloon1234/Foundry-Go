package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/command"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type member struct{}

func subjectRef() model.Reference[subject, int64] {
	return model.NewReference[subject]("subjects", int64(7), codec.Signed[int64]())
}

// auditSchema creates one retained namespace and applies the selected number of
// audit migrations inside it. No schema or data is dropped afterwards.
func auditSchema(t *testing.T, applied int) (*database.DB, string, func(context.Context, func(*database.Tx) error) error) {
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
	applyMigrations(t, within, audit.Migrations()[:applied])
	return db, namespace, within
}

func applyMigrations(t *testing.T, within func(context.Context, func(*database.Tx) error) error, definitions []migrate.Definition) {
	t.Helper()
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, definition := range definitions {
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
}

// recordName captures one model operation on subjects.name through the real
// capture boundary and the recorder's storage path.
func recordName(ctx context.Context, tx *database.Tx, recorder *audit.Recorder, operation lifecycle.Operation, before, after value.Optional[string]) error {
	ids := codec.Signed[int64]()
	b, err := record.NewBuilder(subjectRef(), operation, "id", record.Automatic)
	if err != nil {
		return err
	}
	idBefore, idAfter := value.Set(int64(7)), value.Set(int64(7))
	if operation == lifecycle.Create {
		idBefore = value.Optional[int64]{}
	}
	id, err := lifecycle.CompareField(ids, idBefore, idAfter, operation == lifecycle.Create)
	if err != nil {
		return err
	}
	if err := record.Capture(b, "id", ids, id, record.Automatic); err != nil {
		return err
	}
	text := codec.String[string]()
	name, err := lifecycle.CompareField(text, before, after, true)
	if err != nil {
		return err
	}
	if err := record.Capture(b, "name", text, name, record.Automatic); err != nil {
		return err
	}
	item, err := b.Build()
	if err != nil {
		return err
	}
	return recorder.RecordModel(ctx, tx, item.Entry())
}

func attributed(t *testing.T, requestID attribution.RequestID) context.Context {
	t.Helper()
	actor, err := model.NewReference[member]("members", int64(9), codec.Signed[int64]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithIdentity(actor)
	if err != nil {
		t.Fatal(err)
	}
	origin, err = origin.WithRequest(attribution.Request{ID: requestID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = attribution.WithRoute(ctx, attribution.Route{Method: "PATCH", Name: "subjects.update"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestAuditHistoryKeepsTransactionOrderAndPagesByKeyset(t *testing.T) {
	_, _, within := auditSchema(t, len(audit.Migrations()))
	recorder := newRecorder(t)
	ctx := attributed(t, "request-order")
	action := audit.Define[approved]("record.approved", 1)
	if err := within(ctx, func(tx *database.Tx) error {
		steps := []struct {
			operation     lifecycle.Operation
			before, after value.Optional[string]
		}{
			{lifecycle.Create, value.Optional[string]{}, value.Set("a")},
			{lifecycle.Update, value.Set("a"), value.Set("b")},
			{lifecycle.Update, value.Set("b"), value.Set("c")},
		}
		for _, step := range steps {
			if err := recordName(ctx, tx, recorder, step.operation, step.before, step.after); err != nil {
				return err
			}
		}
		_, err := audit.RecordFor(ctx, tx, recorder, action, subjectRef(), approved{Labels: []string{"x"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		page, err := audit.ModelHistory(ctx, tx, recorder, subjectRef(), query.PageRequest{Number: 1, Size: 10})
		if err != nil {
			return err
		}
		if len(page.Items) != 3 || page.Total != 3 {
			return errors.New("model history lost rows")
		}
		for i, want := range []lifecycle.Operation{lifecycle.Update, lifecycle.Update, lifecycle.Create} {
			if page.Items[i].Changes().Operation() != want {
				return errors.New("rows written in one transaction were misordered")
			}
		}
		if page.Items[0].Sequence() <= page.Items[1].Sequence() || page.Items[1].Sequence() <= page.Items[2].Sequence() {
			return errors.New("sequence does not follow insertion order")
		}
		latest := page.Items[0]
		if correlation, _ := latest.Correlation().Get(); correlation != "request-order" {
			return errors.New("request correlation was not stored")
		}
		if route, present := latest.Route().Get(); !present || route.Method != "PATCH" || route.Name != "subjects.update" {
			return errors.New("matched route was not stored")
		}
		first, err := audit.ModelTimeline(ctx, tx, recorder, subjectRef(), audit.TimelineRequest{Size: 2})
		if err != nil {
			return err
		}
		next, present := first.Next.Get()
		if len(first.Items) != 2 || !present || first.Items[0].ID() != latest.ID() {
			return errors.New("first keyset page is wrong")
		}
		second, err := audit.ModelTimeline(ctx, tx, recorder, subjectRef(), audit.TimelineRequest{Size: 2, Before: value.Set(next)})
		if err != nil {
			return err
		}
		if len(second.Items) != 1 || second.Next.IsSet() || second.Items[0].Changes().Operation() != lifecycle.Create {
			return errors.New("second keyset page is wrong")
		}
		if _, err := audit.ModelTimeline(ctx, tx, recorder, subjectRef(), audit.TimelineRequest{}); !errors.Is(err, fault.Invalid) {
			return errors.New("unbounded timeline accepted")
		}
		activity, err := audit.SubjectActivity(ctx, tx, recorder, subjectRef(), audit.TimelineRequest{Size: 10})
		if err != nil {
			return err
		}
		if len(activity.Items) != 4 || !activity.Items[0].IsAction() {
			return errors.New("subject activity did not combine model and domain rows")
		}
		actionID, ok := audit.ActionActivityID(activity.Items[0], action)
		if !ok {
			return errors.New("activity lost its action identity")
		}
		if found, err := action.Find(ctx, tx, recorder, actionID); err != nil || !found.IsSet() {
			return errors.New("activity action ID did not resolve")
		}
		modelID, ok := audit.ModelActivityID(activity.Items[1], subjectRef())
		if !ok || modelID != latest.ID() {
			return errors.New("activity lost its model identity")
		}
		if _, ok := audit.ModelActivityID(activity.Items[1], model.NewReference[member]("members", int64(0), codec.Signed[int64]())); ok {
			return errors.New("activity converted to a foreign model")
		}
		actor := model.NewReference[member]("members", int64(9), codec.Signed[int64]())
		byActor, err := audit.ActorActivity(ctx, tx, recorder, actor, audit.TimelineRequest{Size: 10})
		if err != nil || len(byActor.Items) != 4 {
			return errors.New("actor activity lost rows")
		}
		other := model.NewReference[member]("members", int64(10), codec.Signed[int64]())
		if none, err := audit.ActorActivity(ctx, tx, recorder, other, audit.TimelineRequest{Size: 10}); err != nil || len(none.Items) != 0 {
			return errors.New("actor activity crossed actors")
		}
		byRequest, err := audit.CorrelationActivity(ctx, tx, recorder, "request-order", audit.TimelineRequest{Size: 10})
		if err != nil || len(byRequest.Items) != 4 {
			return errors.New("correlation activity lost rows")
		}
		actions, err := action.Timeline(ctx, tx, recorder, audit.TimelineRequest{Size: 10})
		if err != nil || len(actions.Items) != 1 {
			return errors.New("action timeline lost rows")
		}
		forSubject, err := audit.ActionTimelineFor(ctx, tx, recorder, action, subjectRef(), audit.TimelineRequest{Size: 10})
		if err != nil || len(forSubject.Items) != 1 || forSubject.Items[0].ID() != actionID {
			return errors.New("subject action timeline is wrong")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditCorrelationAndSystemActivity(t *testing.T) {
	_, _, within := auditSchema(t, len(audit.Migrations()))
	recorder := newRecorder(t)
	origin, err := (attribution.Origin{}).WithSystem("imports.nightly")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := audit.NewCorrelation()
	if err != nil {
		t.Fatal(err)
	}
	batched, err := audit.WithCorrelation(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.WithCorrelation(ctx, " padded"); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid correlation accepted", err)
	}
	action := audit.Define[approved]("import.row", 1)
	if err := within(ctx, func(tx *database.Tx) error {
		for range 3 {
			if _, err := action.Record(batched, tx, recorder, approved{Labels: []string{}}); err != nil {
				return err
			}
		}
		_, err := action.Record(ctx, tx, recorder, approved{Labels: []string{}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		rows, err := audit.CorrelationActivity(ctx, tx, recorder, batch, audit.TimelineRequest{Size: 10})
		if err != nil || len(rows.Items) != 3 {
			return errors.New("explicit correlation did not group the batch")
		}
		if rows.Items[0].Route().IsSet() || rows.Items[0].Correlation() != value.Set(batch) {
			return errors.New("activity metadata changed")
		}
		system, err := audit.SystemActivity(ctx, tx, recorder, "imports.nightly", audit.TimelineRequest{Size: 10})
		if err != nil || len(system.Items) != 4 {
			return errors.New("system activity lost rows")
		}
		if none, err := audit.SystemActivity(ctx, tx, recorder, "imports.other", audit.TimelineRequest{Size: 10}); err != nil || len(none.Items) != 0 {
			return errors.New("system activity crossed systems")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditLargeValuesAreDigestedWithoutFailingTheWrite(t *testing.T) {
	_, _, within := auditSchema(t, len(audit.Migrations()))
	recorder, err := audit.New(audit.Config{Area: "test.audit", MaxValueBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.New(audit.Config{Area: "test.audit", MaxValueBytes: 10}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded value threshold accepted", err)
	}
	large := strings.Repeat("L", 10<<10)
	ctx := t.Context()
	if err := within(ctx, func(tx *database.Tx) error {
		return recordName(ctx, tx, recorder, lifecycle.Create, value.Optional[string]{}, value.Set(large))
	}); err != nil {
		t.Fatal("large audit value failed the business write", err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		page, err := audit.ModelTimeline(ctx, tx, recorder, subjectRef(), audit.TimelineRequest{Size: 1})
		if err != nil || len(page.Items) != 1 {
			return errors.New("digested row was not readable")
		}
		view, err := record.Inspect(page.Items[0].Changes())
		if err != nil {
			return err
		}
		field, err := record.ReadField(view, "name", codec.String[string]())
		if err != nil {
			return err
		}
		stored, _ := field.Get()
		digest, present := stored.After().Snapshot().Digest()
		if !present || digest.Size != int64(len(large)) || digest.SHA256 == "" {
			return errors.New("digest marker was not stored")
		}
		payload, err := page.Items[0].Changes().Entry().Payload()
		if err != nil || strings.Contains(payload, large[:64]) {
			return errors.New("large value was stored")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

const legacyModelPayload = `{"version":1,"primary":"id","fields":[` +
	`{"name":"id","type":2,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"int","t":"7"}}},` +
	`{"name":"session_id","type":4,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"legacy"}}}]}`

func TestAuditMigrationBackfillsOrderingKeysAndKeepsLegacyPolicies(t *testing.T) {
	_, _, within := auditSchema(t, 1)
	subjectJSON := `{"model":"subjects","key":{"k":"int","t":"7"}}`
	origin := `{"subject":{"model":"members","key":{"k":"int","t":"9"}},"request":{"id":"legacy-request"}}`
	// Rows exactly as the previous writer stored them, in this retained schema.
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, row := range []struct{ id, operation, action, version, payload string }{
			{"00000000-0000-7000-8000-000000000002", "0", "legacy.action", "1", `{"session":"legacy-visible"}`},
			{"00000000-0000-7000-8000-000000000001", "1", "", "0", legacyModelPayload},
		} {
			if _, err := tx.Exec(t.Context(), `INSERT INTO foundry_audit (id, area, operation, action, version, subject, payload, redacted, origin, created_at)
VALUES ($1::uuid, 'test.audit', $2::smallint, $3, $4::bigint, $5::jsonb, $6::jsonb, false, $7::jsonb, '2026-01-01T00:00:00Z')`,
				row.id, row.operation, row.action, row.version, subjectJSON, row.payload, origin); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	applyMigrations(t, within, audit.Migrations()[1:])
	recorder := newRecorder(t)
	ctx := attributed(t, "after-migration")
	if err := within(ctx, func(tx *database.Tx) error {
		return recordName(ctx, tx, recorder, lifecycle.Update, value.Set("a"), value.Set("b"))
	}); err != nil {
		t.Fatal(err)
	}
	type legacyAction struct {
		Session string `json:"session"`
	}
	legacy := audit.Define[legacyAction]("legacy.action", 1)
	if err := within(ctx, func(tx *database.Tx) error {
		activity, err := audit.SubjectActivity(ctx, tx, recorder, subjectRef(), audit.TimelineRequest{Size: 10})
		if err != nil {
			return err
		}
		if len(activity.Items) != 3 || activity.Items[0].IsAction() || !activity.Items[1].IsAction() || activity.Items[2].IsAction() {
			return errors.New("backfilled sequence or generated subject key is wrong")
		}
		if activity.Items[2].Sequence() != 1 || activity.Items[1].Sequence() != 2 || activity.Items[0].Sequence() <= 2 {
			return errors.New("new rows did not follow backfilled sequences")
		}
		history, err := audit.ModelHistory(ctx, tx, recorder, subjectRef(), query.PageRequest{Number: 1, Size: 10})
		if err != nil || len(history.Items) != 2 || history.Items[1].Changes().Entry().Redaction() != record.RedactionV1 {
			return errors.New("legacy model history became unreadable")
		}
		id, ok := audit.ActionActivityID(activity.Items[1], legacy)
		if !ok {
			return errors.New("legacy action identity was lost")
		}
		found, err := legacy.Find(ctx, tx, recorder, id)
		if err != nil {
			return err
		}
		row, present := found.Get()
		if !present {
			return errors.New("extended redaction invalidated legacy history")
		}
		// Validated under its own policy 1, then masked under the current one.
		document := row.Document()
		payload, err := document.Payload()
		if err != nil || !document.Redacted() || document.Redaction() != record.CurrentRedaction || strings.Contains(payload, "legacy-visible") {
			return errors.New("legacy action disclosed a value the current policy redacts")
		}
		if _, err := document.Decode(); !errors.Is(err, fault.Missing) {
			return errors.New("masked legacy action decoded into a DTO")
		}
		byRequest, err := audit.CorrelationActivity(ctx, tx, recorder, "legacy-request", audit.TimelineRequest{Size: 10})
		if err != nil || len(byRequest.Items) != 2 {
			return errors.New("request correlation was not backfilled")
		}
		actor := model.NewReference[member]("members", int64(9), codec.Signed[int64]())
		byActor, err := audit.ActorActivity(ctx, tx, recorder, actor, audit.TimelineRequest{Size: 10})
		if err != nil || len(byActor.Items) != 3 {
			return errors.New("generated actor key does not match the Go lookup key")
		}
		// Explicit corruption inside this retained schema: a legacy row claimed
		// by the current policy must fail rather than disclose its value.
		if _, err := tx.Exec(ctx, `UPDATE foundry_audit SET redaction = 2 WHERE id = $1::uuid`, id.String()); err != nil {
			return err
		}
		if _, err := legacy.Find(ctx, tx, recorder, id); err == nil {
			return errors.New("policy mismatch accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditScopePrunesInBoundedTransactions(t *testing.T) {
	db, namespace, within := auditSchema(t, len(audit.Migrations()))
	recorder := newRecorder(t)
	scope, err := audit.NewScope(db, namespace, recorder)
	if err != nil {
		t.Fatal(err)
	}
	action := audit.Define[approved]("record.pruned", 1)
	ctx := t.Context()
	other, err := audit.WithArea(ctx, "retained.area")
	if err != nil {
		t.Fatal(err)
	}
	var retained audit.ActionID[approved]
	if err := within(ctx, func(tx *database.Tx) error {
		for range 5 {
			if _, err := action.Record(ctx, tx, recorder, approved{Labels: []string{}}); err != nil {
				return err
			}
		}
		retained, err = action.Record(other, tx, recorder, approved{Labels: []string{}})
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
	if _, err := scope.PruneRetention(ctx, now, 0); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded prune batch accepted", err)
	}
	// The operator command only counts by default and refuses future cutoffs.
	fixed := fixedClock(now.UTC())
	for _, args := range [][]string{{"audit", "prune"}, {"audit", "prune", "--before", "2020-01-01T00:00:00Z", "--format", "json"}} {
		parsed, err := command.Parse(args, &strings.Builder{})
		if err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		if err := parsed.Run(ctx, scope, fixed, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "matching=5") && !strings.Contains(output.String(), `"applied":false,"matching":5,"removed":0`) {
			t.Fatal("dry run did not count matching entries", output.String())
		}
	}
	future, err := command.Parse([]string{"audit", "prune", "--before", "2026-01-02T00:00:00Z", "--apply"}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	var usage *cli.UsageError
	if err := future.Run(ctx, scope, fixed, &strings.Builder{}); !errors.As(err, &usage) {
		t.Fatal("future prune cutoff accepted", err)
	}
	if count, err := scope.CountBefore(ctx, now); err != nil || count != 5 {
		t.Fatal("dry run or rejected command removed entries", count, err)
	}
	applied, err := command.Parse([]string{"audit", "prune", "--before", "2020-01-01T00:00:00Z", "--batch", "2", "--apply"}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := applied.Run(ctx, scope, fixed, &output); err != nil || output.String() != "removed=5\n" {
		t.Fatal("applied prune", output.String(), err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		_, err := action.Record(ctx, tx, recorder, approved{Labels: []string{}})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE foundry_audit SET created_at = '2000-01-01T00:00:00Z'::timestamptz WHERE area = 'test.audit'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	removed, err := scope.PruneRetention(ctx, now, 2)
	if err != nil || removed != 1 {
		t.Fatal("bounded prune loop removed the wrong number of rows", removed, err)
	}
	if again, err := scope.PruneRetention(ctx, now, 2); err != nil || again != 0 {
		t.Fatal("repeated prune was not idempotent", again, err)
	}
	if err := within(ctx, func(tx *database.Tx) error {
		found, err := action.Find(other, tx, recorder, retained)
		if err != nil || !found.IsSet() {
			return errors.New("prune crossed its area")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	disabled, err := audit.New(audit.Config{Area: "test.audit"})
	if err != nil {
		t.Fatal(err)
	}
	disabledScope, err := audit.NewScope(db, namespace, disabled)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabledScope.PruneRetention(ctx, now, 2); !errors.Is(err, fault.Missing) {
		t.Fatal("disabled retention pruned history", err)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }
