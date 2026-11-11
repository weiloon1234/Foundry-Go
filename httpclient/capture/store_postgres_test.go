package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/capturelog"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Derived from migrate.Registry's canonical definition encoding. A change to
// the shipped migration needs a new migration ID, not a new checksum.
const createMigrationChecksum = "4acb9272ed03d74cf80d211a9e6130444c1ddfbcd861c7e4451f51cb12d24ae8"

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

func TestMigrationIdentityAndConfigValidation(t *testing.T) {
	definitions := Migrations()
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Key != (migrate.Key{Origin: "foundry.outbound_capture", ID: "000001_create_outbound_captures"}) || definitions[0].Version != "v0.19.0" || definitions[0].Mode != migrate.Transactional || len(definitions[0].Down) != 0 {
		t.Fatal("outbound capture migration identity or reversal policy changed")
	}
	if checksum := registry.Entries()[0].Checksum.String(); checksum != createMigrationChecksum {
		t.Fatalf("shipped outbound capture migration changed: %s", checksum)
	}
	definitions[0].SQL[0] = "SELECT 1"
	if Migrations()[0].SQL[0] == "SELECT 1" {
		t.Fatal("Migrations returned shared mutable storage")
	}
	if err := orm.Check(store.Captures.Table); err != nil {
		t.Fatal("outbound capture declaration is invalid", err)
	}
	for _, edit := range []func(*Config){
		func(c *Config) { c.BodyBytes = 512 }, func(c *Config) { c.ValueBytes = 32 }, func(c *Config) { c.Rate = 0 },
		func(c *Config) { c.QueueRows = 257 }, func(c *Config) { c.RetainedBytes = 4 << 20 }, func(c *Config) { c.Retention = time.Minute },
		func(c *Config) { c.MaxRows = 999 }, func(c *Config) { c.MaskNames = []string{""} }, func(c *Config) { c.MaskNames = make([]string, 65) },
		func(c *Config) { c.Exclude = []Exclusion{{}} }, func(c *Config) { c.Exclude = []Exclusion{{PathPrefix: "/"}} },
		func(c *Config) { c.Exclude = []Exclusion{{Client: "a"}, {Client: "a"}} }, func(c *Config) { c.Exclude = []Exclusion{{Host: "*"}} },
		func(c *Config) { c.Exclude = []Exclusion{{Host: "https://api.test"}} }, func(c *Config) { c.Exclude = []Exclusion{{Host: "api.test:443"}} },
		func(c *Config) { c.Exclude = []Exclusion{{Host: "api.test/v1"}} }, func(c *Config) { c.Exclude = []Exclusion{{Method: "get"}} },
		func(c *Config) { c.Exclude = []Exclusion{{PathPrefix: "relative"}} }, func(c *Config) { c.Exclude = []Exclusion{{PathPrefix: "/api/"}} },
		func(c *Config) { c.Exclude = []Exclusion{{Client: "bad name!"}} },
		func(c *Config) { c.Exclude = make([]Exclusion, 65) },
	} {
		config := DefaultConfig()
		edit(&config)
		if !errors.Is(config.Validate(), fault.Invalid) {
			t.Fatal("invalid outbound capture config accepted", config.Exclude)
		}
		if _, err := NewRecorder(config, nil); err == nil {
			t.Fatal("recorder accepted an invalid config")
		}
	}
	valid := DefaultConfig()
	valid.MaskNames = []string{"PIN_Hint"}
	valid.Exclude = []Exclusion{{Client: "crawler"}, {Host: "*.Ingest.Sentry.io"}, {Client: "visibility", Method: "GET", PathPrefix: "/api/v1/models"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRecorder(valid, []httpclient.Name{"bad name!"}); err == nil {
		t.Fatal("full-body declaration accepted an invalid client name")
	}
	guard := foundryhttp.GuardBinding[struct{}]{}
	if _, err := Routes(foundryhttp.DefineScope("/developer", "developer"), nil, guard, foundryhttp.Permissions[struct{}]()); !errors.Is(err, fault.Invalid) {
		t.Fatal("routes accepted a nil store")
	}
	if _, err := Routes[struct{}](foundryhttp.DefineScope("/developer", "developer"), &Store{}, guard); !errors.Is(err, fault.Invalid) {
		t.Fatal("routes accepted no access requirement")
	}
}

type outboundFixture struct {
	db     *database.DB
	schema string
	store  *Store
	clock  *testClock
}

func newOutboundFixture(t *testing.T, recorder *httpcapture.Outbound, config Config) outboundFixture {
	t.Helper()
	scope := pgtest.Isolate(t)
	f := outboundFixture{db: scope.Open(t), schema: scope.Schema(), clock: newTestClock(time.Now())}
	registry, err := migrate.New(Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	migration := migrate.DefaultPostgresConfig()
	migration.Schema, migration.SearchPath = f.schema, f.schema
	runner, err := migrate.NewPostgres(f.db, registry, migration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.store, err = New(recorder, Dependencies{DB: f.db, Schema: f.schema, Clock: f.clock, Logger: discardLogger()}, config); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f outboundFixture) count(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := database.ScanOne(t.Context(), f.db.Primary(), `SELECT count(*) FROM "`+f.schema+`".foundry_outbound_captures`+where, args, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f outboundFixture) insert(t *testing.T, rows []store.Capture) error {
	t.Helper()
	return f.store.log.Transaction(t.Context(), false, func(tx *database.Tx) error {
		for start := 0; start < len(rows); start += 500 {
			if err := store.Insert(t.Context(), tx, rows[start:min(start+500, len(rows))]); err != nil {
				return err
			}
		}
		return nil
	})
}

// exchange is a minimal completed attempt for retention and CHECK tests.
func exchange(t *testing.T, client string, at time.Time) httpcapture.Exchange {
	t.Helper()
	id, err := model.NewIDAt[Exchange](at)
	if err != nil {
		t.Fatal(err)
	}
	return httpcapture.Exchange{ID: id.Bytes(), Operation: id.Bytes(), Client: client, Attempt: 1, StartedAt: at, Method: "GET", Scheme: "https", Host: "api.test", EscapedPath: "/v1",
		Status: value.Of(uint16(200)), HeadersAfter: value.Of(time.Millisecond), Request: httpcapture.BodyCapture{Complete: true}, Response: httpcapture.BodyCapture{Complete: true}}
}

func row(t *testing.T, e httpcapture.Exchange) store.Capture {
	t.Helper()
	r, err := redact(e, 0, DefaultConfig().policy())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPostgresChecksMatchGoEnumsAndRejectIllegalRows(t *testing.T) {
	f := newOutboundFixture(t, nil, DefaultConfig())
	stream, err := f.db.Primary().Query(t.Context(), `SELECT pg_catalog.pg_get_constraintdef(con.oid) FROM pg_catalog.pg_constraint con JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid JOIN pg_catalog.pg_namespace ns ON ns.oid = rel.relnamespace WHERE ns.nspname = $1 AND rel.relname = 'foundry_outbound_captures' AND con.contype = 'c'`, f.schema)
	if err != nil {
		t.Fatal(err)
	}
	var definitions []string
	for stream.Next() {
		var definition string
		if err := stream.Scan(&definition); err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, definition)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	literal := regexp.MustCompile(`'([a-z_]+)'::text`)
	listed := func(column string) []string {
		for _, definition := range definitions {
			if strings.HasPrefix(definition, "CHECK (("+column+" = ANY") {
				var values []string
				for _, match := range literal.FindAllStringSubmatch(definition, -1) {
					values = append(values, match[1])
				}
				slices.Sort(values)
				return values
			}
		}
		t.Fatal("missing CHECK for", column)
		return nil
	}
	strs := func(values any) []string {
		var result []string
		encoded, _ := json.Marshal(values)
		_ = json.Unmarshal(encoded, &result)
		slices.Sort(result)
		return result
	}
	for column, want := range map[string][]string{
		"result": strs(OutboundResultValues()), "body_mode": strs(OutboundCaptureModeValues()),
		"request_body_state": strs(OutboundBodyStateValues()), "response_body_state": strs(OutboundBodyStateValues()),
		"request_body_omission": strs(OutboundBodyOmissionValues()), "response_body_omission": strs(OutboundBodyOmissionValues()),
	} {
		if got := listed(column); !slices.Equal(got, want) {
			t.Errorf("%s CHECK %v differs from Go values %v", column, got, want)
		}
	}
	var omissions []string
	for _, definition := range definitions {
		if match := regexp.MustCompile(`'(\[[^']*\])'::jsonb`).FindStringSubmatch(definition); len(match) == 2 {
			_ = json.Unmarshal([]byte(match[1]), &omissions)
		}
	}
	slices.Sort(omissions)
	if !slices.Equal(omissions, strs(OutboundCaptureOmissionValues())) {
		t.Error("omissions CHECK differs from OutboundCaptureOmission values", omissions)
	}

	notSent := exchange(t, "api", testInstant(0))
	notSent.Attempt, notSent.Result, notSent.Status, notSent.HeadersAfter = 0, httpcapture.ResultDestinationDenied, value.Nullable[uint16]{}, value.Nullable[time.Duration]{}
	notSent.Request = httpcapture.BodyCapture{Skip: httpcapture.SkipNotSent}
	if err := f.insert(t, []store.Capture{row(t, notSent)}); err != nil {
		t.Fatal("attempt-0 not_sent row was rejected", err)
	}
	dialDenied := exchange(t, "api", testInstant(1))
	dialDenied.Result, dialDenied.Status, dialDenied.HeadersAfter = httpcapture.ResultDestinationDenied, value.Nullable[uint16]{}, value.Nullable[time.Duration]{}
	if err := f.insert(t, []store.Capture{row(t, dialDenied)}); err != nil {
		t.Fatal("attempt-1 dial denial row was rejected", err)
	}
	for name, edit := range map[string]func(*store.Capture){
		"not_sent after attempt 0": func(c *store.Capture) { c.Attempt = 1 },
		"attempt 0 without not_sent": func(c *store.Capture) {
			c.RequestBodyState, c.RequestBodyOmission = string(BodyEmpty), value.Nullable[string]{}
		},
		"overloaded on the wire": func(c *store.Capture) { c.Attempt, c.Result = 1, string(ResultOverloaded) },
		"status without headers": func(c *store.Capture) { c.Status = value.Of(int16(200)) },
		"redacted exact body": func(c *store.Capture) {
			c.Attempt, c.RequestBodyState, c.RequestBodyOmission, c.RequestBody = 1, string(BodyExact), value.Nullable[string]{}, []byte("x")
		},
	} {
		bad := row(t, notSent)
		id, _ := model.NewIDAt[store.Capture](testInstant(2))
		bad.ID = id
		edit(&bad)
		if err := f.insert(t, []store.Capture{bad}); err == nil {
			t.Error("illegal row accepted:", name)
		}
	}
}

func TestPostgresWriterListDetailAndSiblings(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/flaky" && calls.Add(1) == 1 {
			w.WriteHeader(502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, DefaultConfig(), testClientConfig(server.URL+"/v1"), nil)
	f := newOutboundFixture(t, h.recorder, DefaultConfig())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.store.Run(ctx) }()
	if _, err := h.client.Do(t.Context(), h.client.Get("flaky")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.client.Do(t.Context(), h.client.Post("orders").Header("Content-Type", "application/json").WithBody(httpclient.Bytes([]byte(`{"card":"4242","password":"p"}`)))); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for f.count(t, "") != 3 {
		if time.Now().After(deadline) {
			t.Fatal("writer did not store every attempt")
		}
		time.Sleep(20 * time.Millisecond)
	}
	from, _ := temporal.NewDateTime(time.Now().Add(-time.Hour))
	page, err := f.store.list(t.Context(), OutboundCaptureFilters{From: from}, orm.CursorRequest[store.Summary]{Size: 30})
	if err != nil || len(page.Items()) != 3 || page.Items()[0].Method != "POST" {
		t.Fatal("newest-first list changed", err)
	}
	for filters, want := range map[*OutboundCaptureFilters]int{
		{From: from, Problems: value.Set(true)}:                                            1,
		{From: from, Status: value.Set(int32(502))}:                                        1,
		{From: from, Method: value.Set("POST"), Client: value.Set(httpclient.Name("api"))}: 1,
		{From: from, Result: value.Set(ResultCompleted)}:                                   3,
		{From: from, Operation: value.Set(page.Items()[2].OperationID)}:                    2,
		{From: from, Host: value.Set("nowhere.test")}:                                      0,
	} {
		page, err := f.store.list(t.Context(), *filters, orm.CursorRequest[store.Summary]{Size: 30})
		if err != nil || len(page.Items()) != want {
			t.Fatal("filtered list changed", err, len(page.Items()), want)
		}
	}
	for _, filters := range []OutboundCaptureFilters{{}, {From: from, Status: value.Set(int32(42))}} {
		if _, err := f.store.list(t.Context(), filters, orm.CursorRequest[store.Summary]{Size: 30}); !errors.Is(err, foundryhttp.BadRequest) {
			t.Fatal("unbounded window or invalid status accepted", err)
		}
	}
	retry := page.Items()[2]
	detail, err := f.store.find(t.Context(), retry.ID)
	if err != nil || len(detail.Attempts) != 1 || detail.Attempts[0].Attempt != 2 || !detail.Capture.Retried || detail.ResponseBody.State != BodyEmpty {
		t.Fatal("detail lost its operation's other attempt", err)
	}
	order, err := f.store.find(t.Context(), page.Items()[0].ID)
	if err != nil || string(order.RequestBody.Content) != `{"card":"4242","password":"[masked]"}` || len(order.Attempts) != 0 || order.ResponseBody.State != BodyJSON {
		t.Fatal("detail lost redacted content", err)
	}
	unknown, _ := model.NewIDAt[Exchange](time.Now())
	if _, err := f.store.find(t.Context(), unknown); !errors.Is(err, foundryhttp.NotFound) {
		t.Fatal("unknown capture was not 404", err)
	}
	f.clock.Set(time.Now().Add(DefaultConfig().Retention + time.Hour))
	if _, err := f.store.find(t.Context(), retry.ID); !errors.Is(err, foundryhttp.NotFound) {
		t.Fatal("expired capture remained readable")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// The row cap applies per client: keyset iteration reaches every client,
// however many there are, and a chatty client never evicts another's rows.
func TestPostgresRetentionCapsEachClientAndPruneCommand(t *testing.T) {
	config := DefaultConfig()
	config.MaxRows = 1_000
	f := newOutboundFixture(t, nil, config)
	f.clock.Set(testInstant(30))
	var rows []store.Capture
	for i := range 300 {
		rows = append(rows, row(t, exchange(t, fmt.Sprintf("c%03d", i), testInstant(0).Add(time.Duration(i)*time.Millisecond))))
	}
	for i := range 1_004 {
		rows = append(rows, row(t, exchange(t, "zz-chatty", testInstant(1).Add(time.Duration(i)*time.Millisecond))))
	}
	expired := testInstant(30).Add(-config.Retention - time.Hour)
	rows = append(rows, row(t, exchange(t, "c000", expired)))
	if err := f.insert(t, rows); err != nil {
		t.Fatal(err)
	}
	if err := f.insert(t, rows[:1]); err != nil {
		t.Fatal("replayed capture ID was not skipped", err)
	}
	estimate, err := f.store.log.Estimate(t.Context(), mustCutoff(t, f), true)
	if err != nil || estimate != 5 {
		t.Fatal("per-client estimate changed", estimate, err)
	}
	run := func(args ...string) string {
		command, err := capturelog.ParsePrune("outbound-captures", args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		if err := command.Run(t.Context(), f.store.log, &output); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	if out := run("prune"); out != "5 outbound captures match; rerun with --apply to remove them\n" || f.count(t, "") != 1_305 {
		t.Fatal("dry run changed rows or its report", out)
	}
	if out := run("prune", "--apply", "--batch", "2"); out != "removed 5 outbound captures\n" {
		t.Fatal("apply report changed", out)
	}
	if f.count(t, ` WHERE client = 'zz-chatty'`) != 1_000 || f.count(t, ` WHERE client <> 'zz-chatty'`) != 300 {
		t.Fatal("cap did not keep each client's newest rows")
	}
	err = f.store.log.Transaction(t.Context(), false, func(tx *database.Tx) error {
		locked, err := f.store.log.Lock(t.Context(), tx)
		if err != nil || !locked {
			return fmt.Errorf("lock not taken: %v", err)
		}
		_, _, err = f.store.log.Prune(t.Context(), mustCutoff(t, f), true, 1, 1)
		return err
	})
	if !errors.Is(err, capturelog.ErrRetentionBusy) {
		t.Fatal("concurrent retention did not yield to the lock holder", err)
	}
}

func mustCutoff(t *testing.T, f outboundFixture) temporal.DateTime {
	t.Helper()
	cutoff, err := f.store.log.Cutoff()
	if err != nil {
		t.Fatal(err)
	}
	return cutoff
}
