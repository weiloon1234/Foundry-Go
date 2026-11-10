package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Derived from migrate.Registry's canonical definition encoding. A change to
// the shipped migration needs a new migration ID, not a new checksum.
const captureMigrationChecksum = "3be40b3a8c510d49bfa598b9ce05ba6a38c3572491c0929fedc6826fa279877f"

type testClock struct{ now atomic.Pointer[time.Time] }

func newTestClock(at time.Time) *testClock {
	c := &testClock{}
	c.now.Store(&at)
	return c
}
func (c *testClock) Now() time.Time    { return *c.now.Load() }
func (c *testClock) Set(at time.Time)  { c.now.Store(&at) }
func discardLogger() *slog.Logger      { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func testInstant(minute int) time.Time { return time.Date(2026, 11, 11, 1, minute, 0, 0, time.UTC) }
func testID(i int) attribution.RequestID {
	return attribution.RequestID(fmt.Sprintf("0192f3a4-5b6c-7d8e-9f01-%012x", i))
}

func TestCaptureMigrationIdentity(t *testing.T) {
	definitions := Migrations()
	if len(definitions) != 1 || definitions[0].Key != (migrate.Key{Origin: "foundry.webhook_capture", ID: "000001_create_webhook_captures"}) || definitions[0].Mode != migrate.Transactional || len(definitions[0].Down) != 0 {
		t.Fatal("capture migration identity or reversal policy changed")
	}
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	if checksum := registry.Entries()[0].Checksum.String(); checksum != captureMigrationChecksum {
		t.Fatalf("shipped capture migration changed: %s", checksum)
	}
	definitions[0].SQL[0] = "SELECT 1"
	if Migrations()[0].SQL[0] == "SELECT 1" {
		t.Fatal("Migrations returned shared mutable storage")
	}
	if err := orm.Check(store.Captures.Table); err != nil {
		t.Fatal("capture declaration is invalid", err)
	}
}

func TestConfigAndRoutesValidation(t *testing.T) {
	for _, edit := range []func(*Config){
		func(c *Config) { c.BodyBytes = 512 }, func(c *Config) { c.BodyBytes = 9 << 20 }, func(c *Config) { c.ValueBytes = 32 },
		func(c *Config) { c.Rate = 0 }, func(c *Config) { c.QueueRows = 257 }, func(c *Config) { c.RetainedBytes = 1 << 10 },
		func(c *Config) { c.Retention = time.Minute }, func(c *Config) { c.MaxRows = 999 },
	} {
		config := DefaultConfig()
		edit(&config)
		if !errors.Is(config.Validate(), fault.Invalid) {
			t.Fatal("out-of-range capture config accepted")
		}
		if _, err := NewRecorder(config); err == nil {
			t.Fatal("recorder accepted an invalid config")
		}
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	guard := http.GuardBinding[routeActor]{}
	if _, err := Routes(http.DefineScope("/developer", "developer"), nil, guard, http.Permissions(developerRead)); !errors.Is(err, fault.Invalid) {
		t.Fatal("routes accepted a nil store")
	}
	if _, err := Routes[routeActor](http.DefineScope("/developer", "developer"), &Store{}, guard); !errors.Is(err, fault.Invalid) {
		t.Fatal("routes accepted no access requirement")
	}
}

type captureFixture struct {
	db       *database.DB
	schema   string
	recorder *httpcapture.Recorder
	store    *Store
	clock    *testClock
}

func newCaptureFixture(t *testing.T, migrated bool) captureFixture {
	t.Helper()
	scope := pgtest.Isolate(t)
	f := captureFixture{db: scope.Open(t), schema: scope.Schema(), clock: newTestClock(testInstant(30))}
	if migrated {
		f.migrate(t)
	}
	var err error
	if f.recorder, err = NewRecorder(DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if f.store, err = New(f.recorder, Dependencies{DB: f.db, Schema: f.schema, Clock: f.clock, Logger: discardLogger()}, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f captureFixture) migrate(t *testing.T) {
	t.Helper()
	registry, err := migrate.New(Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema, config.SearchPath = f.schema, f.schema
	runner, err := migrate.NewPostgres(f.db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (f captureFixture) complete(t *testing.T, s httpcapture.Snapshot) {
	t.Helper()
	reservation := f.recorder.Reserve()
	if reservation == nil {
		t.Fatal("recorder refused a test capture")
	}
	reservation.Complete(s)
}

func (f captureFixture) count(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.store.scope.Transaction(t.Context(), true, func(tx *database.Tx) error {
		var err error
		n, err = store.Count(t.Context(), tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPostgresCaptureChecksMatchGoEnums(t *testing.T) {
	f := newCaptureFixture(t, true)
	stream, err := f.db.Primary().Query(t.Context(), `SELECT con.conname, pg_catalog.pg_get_constraintdef(con.oid) FROM pg_catalog.pg_constraint con JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid JOIN pg_catalog.pg_namespace ns ON ns.oid = rel.relnamespace WHERE ns.nspname = $1 AND rel.relname = 'foundry_webhook_captures' AND con.contype = 'c'`, f.schema)
	if err != nil {
		t.Fatal(err)
	}
	definitions := map[string]string{}
	for stream.Next() {
		var name, definition string
		if err := stream.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		definitions[name] = definition
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	literal := regexp.MustCompile(`'([a-z_]+)'::text`)
	listed := func(column string) []string {
		var values []string
		for _, match := range literal.FindAllStringSubmatch(definitions["foundry_webhook_captures_"+column+"_check"], -1) {
			values = append(values, match[1])
		}
		slices.Sort(values)
		return values
	}
	names := func(values ...string) []string { slices.Sort(values); return values }
	strs := func(values any) []string {
		var result []string
		encoded, _ := json.Marshal(values)
		_ = json.Unmarshal(encoded, &result)
		return names(result...)
	}
	for column, want := range map[string][]string{
		"body_mode":      strs(CaptureModeValues()),
		"body_state":     strs(CaptureBodyStateValues()),
		"body_omission":  strs(CaptureBodyOmissionValues()),
		"response_state": strs(CaptureResponseStateValues()),
		"outcome":        names(string(observability.Succeeded), string(observability.Failed), string(observability.Rejected), string(observability.Cancelled), string(observability.TimedOut), string(observability.Panicked)),
	} {
		if got := listed(column); !slices.Equal(got, want) {
			t.Errorf("%s CHECK %v differs from Go values %v", column, got, want)
		}
	}
	omissions := regexp.MustCompile(`'(\[[^']*\])'::jsonb`).FindStringSubmatch(definitions["foundry_webhook_captures_omissions_check"])
	var stored []string
	if len(omissions) != 2 || json.Unmarshal([]byte(omissions[1]), &stored) != nil || !slices.Equal(names(stored...), strs(CaptureOmissionValues())) {
		t.Error("omissions CHECK differs from CaptureOmission values", omissions)
	}
	var indexes string
	if err := database.ScanOne(t.Context(), f.db.Primary(), `SELECT string_agg(indexdef, ';' ORDER BY indexname) FROM pg_catalog.pg_indexes WHERE schemaname = $1 AND tablename = 'foundry_webhook_captures'`, []any{f.schema}, &indexes); err != nil {
		t.Fatal(err)
	}
	for _, columns := range []string{"(method, received_at DESC, id DESC)", "(received_at DESC, id DESC)", "(response_status, received_at DESC, id DESC)"} {
		if !strings.Contains(indexes, columns) {
			t.Error("missing capture index", columns)
		}
	}
}

func TestPostgresCaptureWriterReadsAndShutdownDrain(t *testing.T) {
	f := newCaptureFixture(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.store.Run(ctx) }()
	first := snapshot("application/json", []byte(`{"token":"secret","amount":12}`))
	first.Metadata.Headers = []httpcapture.Field{{Name: "Authorization", Values: []string{"Bearer x"}}}
	f.complete(t, first)
	second := snapshot("application/octet-stream", []byte{0, 1, 2})
	second.Metadata.RequestID, second.Metadata.ReceivedAt, second.Metadata.Full = testID(2), testInstant(1), true
	second.Completion.Status, second.Completion.Result = value.Nullable[uint16]{}, observability.Result{Outcome: observability.Panicked}
	f.complete(t, second)
	deadline := time.Now().Add(10 * time.Second)
	for f.count(t) != 2 {
		if time.Now().After(deadline) {
			t.Fatal("writer did not store both captures")
		}
		time.Sleep(20 * time.Millisecond)
	}
	page, err := f.store.list(t.Context(), CapturedRequestFilters{From: dateTime(t, testInstant(0))}, orm.CursorRequest[store.Summary]{Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	items := page.Items()
	next, more := page.Next().Get()
	if len(items) != 1 || items[0].RequestID != testRequestID || !more {
		t.Fatal("newest-first page changed", items)
	}
	page, err = f.store.list(t.Context(), CapturedRequestFilters{From: dateTime(t, testInstant(0))}, orm.CursorRequest[store.Summary]{Size: 1, After: value.Set(next)})
	if err != nil || len(page.Items()) != 1 || page.Items()[0].RequestID != testID(2) || page.Items()[0].Mode != Full || !page.Items()[0].Status.IsNull() {
		t.Fatal("cursor page lost its position or row facts", err)
	}
	for _, filters := range []CapturedRequestFilters{
		{From: dateTime(t, testInstant(0)), Mode: value.Set(Full)},
		{From: dateTime(t, testInstant(0)), Status: value.Set(int32(200)), Method: value.Set("POST"), PathPrefix: value.Set("/wh/str")},
		{From: dateTime(t, testInstant(0)), Until: value.Set(dateTime(t, testInstant(2)))},
	} {
		page, err := f.store.list(t.Context(), filters, orm.CursorRequest[store.Summary]{Size: 30})
		if err != nil || len(page.Items()) != 1 {
			t.Fatal("filtered list changed", err, len(page.Items()))
		}
	}
	for _, filters := range []CapturedRequestFilters{{}, {From: dateTime(t, testInstant(0)), Until: value.Set(dateTime(t, testInstant(0)))}, {From: dateTime(t, testInstant(0).Add(-31*24*time.Hour))}} {
		if _, err := f.store.list(t.Context(), filters, orm.CursorRequest[store.Summary]{Size: 30}); !errors.Is(err, http.BadRequest) {
			t.Fatal("unbounded or inverted window accepted", err)
		}
	}
	detail, err := f.store.find(t.Context(), testRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if string(detail.Body) != `{"token":"[masked]","amount":12}` || detail.Headers[0].Values[0] != maskedValue || string(detail.ResponseHead) != `{"ok":true}` || detail.Request.BodyState != BodyJSON {
		t.Fatal("detail lost redacted content", string(detail.Body))
	}
	for _, id := range []attribution.RequestID{"not-a-uuid", testID(9)} {
		if _, err := f.store.find(t.Context(), id); !errors.Is(err, http.NotFound) {
			t.Fatal("unknown capture was not 404", err)
		}
	}
	f.clock.Set(testInstant(30).Add(DefaultConfig().Retention))
	if _, err := f.store.find(t.Context(), testID(2)); !errors.Is(err, http.NotFound) {
		t.Fatal("expired capture remained readable")
	}
	f.clock.Set(testInstant(30))
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	third := snapshot("application/json", []byte(`{}`))
	third.Metadata.RequestID = testID(3)
	f.complete(t, third)
	if err := f.store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-f.store.Done()
	if f.count(t) != 3 || f.recorder.Reserve() != nil {
		t.Fatal("shutdown did not drain the queue or seal admission")
	}
}

func TestPostgresCaptureStoresEmptyBodiesAndResponses(t *testing.T) {
	f := newCaptureFixture(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.store.Run(ctx) }()
	ping := snapshot("", nil)
	ping.Metadata.Method = "GET"
	ping.Completion.Route, ping.Completion.Status = "", value.Of(uint16(404))
	f.complete(t, ping)
	noContent := snapshot("application/json", []byte(`{"id":"evt_2"}`))
	noContent.Metadata.RequestID = testID(2)
	noContent.Completion.Status, noContent.Completion.ResponseHead, noContent.Completion.ResponseBytes = value.Of(uint16(204)), nil, 0
	f.complete(t, noContent)
	deadline := time.Now().Add(10 * time.Second)
	for f.count(t) != 2 {
		if time.Now().After(deadline) {
			t.Fatal("a capture with an empty body or response was not stored")
		}
		time.Sleep(20 * time.Millisecond)
	}
	detail, err := f.store.find(t.Context(), testRequestID)
	if err != nil || len(detail.Body) != 0 || detail.Request.BodyState != BodyEmpty {
		t.Fatal("empty request body changed", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPostgresCaptureUnavailableTableDegradesAndCarriesLoss(t *testing.T) {
	f := newCaptureFixture(t, false)
	f.store.check(t.Context())
	f.complete(t, snapshot("application/json", []byte(`{}`)))
	f.store.flush(t.Context(), t.Context())
	if f.recorder.Drops()[httpcapture.DropWrite] != 1 {
		t.Fatal("capture without a table was not counted as lost")
	}
	if active, retained := f.recorder.Usage(); active != 0 || retained != 0 {
		t.Fatal("lost capture still holds recorder capacity", active, retained)
	}
	f.migrate(t)
	f.store.check(t.Context())
	next := snapshot("application/json", []byte(`{}`))
	next.Metadata.RequestID = testID(4)
	f.complete(t, next)
	f.store.flush(t.Context(), t.Context())
	detail, err := f.store.find(t.Context(), testID(4))
	if err != nil || detail.Request.DroppedBefore != 1 {
		t.Fatal("next stored capture did not report the earlier loss", err)
	}
}

func TestPostgresCaptureRetentionByAgeThenRowCap(t *testing.T) {
	f := newCaptureFixture(t, true)
	f.store.config.MaxRows = 1_000
	const total = 1_010
	rows := make([]store.Capture, 0, total)
	for i := range total {
		s := snapshot("application/json", []byte(`{}`))
		s.Metadata.RequestID, s.Metadata.ReceivedAt = testID(100+i), testInstant(0).Add(-time.Duration(total-i)*time.Second)
		row, err := redact(s, f.store.policy)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := f.store.scope.Transaction(t.Context(), false, func(tx *database.Tx) error {
		if err := store.Insert(t.Context(), tx, rows[:1000]); err != nil {
			return err
		}
		if err := store.Insert(t.Context(), tx, rows[1000:]); err != nil {
			return err
		}
		return store.Insert(t.Context(), tx, rows[:1])
	}); err != nil {
		t.Fatal("insert or replayed request ID failed", err)
	}
	cutoff := dateTime(t, testInstant(0).Add(-(total-5)*time.Second))
	estimate, err := f.store.pruneEstimate(t.Context(), cutoff, false)
	if err != nil || estimate != 5 {
		t.Fatal("age estimate changed", estimate, err)
	}
	removed, behind, err := f.store.prune(t.Context(), cutoff, false, 2, pruneBatches)
	if err != nil || removed != 5 || behind || f.count(t) != total-5 {
		t.Fatal("age pruning changed", removed, behind, err)
	}
	estimate, err = f.store.pruneEstimate(t.Context(), cutoff, true)
	if err != nil || estimate != 5 {
		t.Fatal("cap estimate changed", estimate, err)
	}
	removed, behind, err = f.store.prune(t.Context(), cutoff, true, 1, 3)
	if err != nil || removed != 2 || !behind {
		t.Fatal("bounded pass did not report it is behind", removed, behind, err)
	}
	f.store.prunePass(t.Context())
	if f.count(t) != 1_000 {
		t.Fatal("scheduled pass did not reach the row cap", f.count(t))
	}
	if _, err := f.store.find(t.Context(), testID(100+total-1)); err != nil {
		t.Fatal("row cap removed the newest capture", err)
	}
	err = f.store.scope.Transaction(t.Context(), false, func(tx *database.Tx) error {
		locked, err := store.Lock(t.Context(), tx, f.schema)
		if err != nil || !locked {
			return fmt.Errorf("lock not taken: %v", err)
		}
		_, _, err = f.store.prune(t.Context(), cutoff, true, 1, 1)
		return err
	})
	if !errors.Is(err, errRetentionBusy) {
		t.Fatal("concurrent retention did not yield to the lock holder", err)
	}
}

func dateTime(t *testing.T, at time.Time) temporal.DateTime {
	t.Helper()
	v, err := temporal.NewDateTime(at)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPostgresCapturePruneCommand(t *testing.T) {
	f := newCaptureFixture(t, true)
	var rows []store.Capture
	for i, at := range []time.Time{testInstant(0).Add(-40 * 24 * time.Hour), testInstant(0).Add(-35 * 24 * time.Hour), testInstant(0)} {
		s := snapshot("application/json", []byte(`{}`))
		s.Metadata.RequestID, s.Metadata.ReceivedAt = testID(500+i), at
		row, err := redact(s, f.store.policy)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := f.store.scope.Transaction(t.Context(), false, func(tx *database.Tx) error { return store.Insert(t.Context(), tx, rows) }); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		command, err := ParsePrune(args, io.Discard)
		if err != nil {
			return "", err
		}
		var output strings.Builder
		err = command.Run(t.Context(), f.store, &output)
		return output.String(), err
	}
	if out, err := run("prune"); err != nil || out != "2 webhook captures match; rerun with --apply to remove them\n" || f.count(t) != 3 {
		t.Fatal("dry run changed rows or its report", out, err)
	}
	if out, err := run("prune", "--apply", "--format", "json", "--batch", "1"); err != nil || out != "{\"applied\":true,\"removed\":2}\n" || f.count(t) != 1 {
		t.Fatal("apply did not remove expired rows", out, err)
	}
	if out, err := run("prune", "--apply", "--before", "2026-11-11T01:01:00Z"); err != nil || out != "removed 1 webhook captures\n" || f.count(t) != 0 {
		t.Fatal("explicit cutoff did not purge", out, err)
	}
	for _, args := range [][]string{{"other"}, {"prune", "--batch", "0"}, {"prune", "--format", "xml"}, {"prune", "extra"}} {
		if _, err := ParsePrune(args, io.Discard); err == nil {
			t.Fatal("invalid prune arguments accepted", args)
		}
	}
	if _, err := run("prune", "--before", "2999-01-01T00:00:00Z"); err == nil {
		t.Fatal("future cutoff accepted")
	}
}
