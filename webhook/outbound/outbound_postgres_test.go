package outbound_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/inline"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/webhook"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound/command"
)

type InvoicePaid struct {
	Invoice string `json:"invoice"`
}

func invoiceContract() contract.JSON[InvoicePaid] {
	typ := reflect.TypeFor[InvoicePaid]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[InvoicePaid](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "invoice", Type: "text", Required: true}}}, {ID: "text", Kind: contract.StringKind}}})
}

var invoicePaid = outbound.DefineEvent[InvoicePaid]("invoice.paid", invoiceContract())

type account int64

// receiver verifies every request with the inbound Standard Webhooks
// verifier and answers with the next scripted status.
type receiver struct {
	mu        sync.Mutex
	server    *httptest.Server
	verifier  *webhook.Verifier[account]
	statuses  []int
	bodyBytes int
	ids       []string
	bodies    []string
	verified  []bool
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	verified := false
	if r.verifier != nil {
		_, err := r.verifier.Verify(request.Context(), request.Header, body)
		verified = err == nil
	}
	r.ids = append(r.ids, request.Header.Get("Webhook-Id"))
	r.bodies = append(r.bodies, string(body))
	r.verified = append(r.verified, verified)
	status := 204
	if len(r.statuses) > 0 {
		status, r.statuses = r.statuses[0], r.statuses[1:]
	}
	if r.bodyBytes > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(r.bodyBytes))
		w.WriteHeader(status)
		_, _ = w.Write(make([]byte, r.bodyBytes))
		return
	}
	w.WriteHeader(status)
}
func (r *receiver) trust(t *testing.T, keys ...secret.String) {
	t.Helper()
	verifier, err := webhook.New(webhook.DefaultConfig("billing", webhook.Standard), account(1), keys, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.verifier = verifier
	r.mu.Unlock()
}
func (r *receiver) script(statuses ...int) {
	r.mu.Lock()
	r.statuses = statuses
	r.mu.Unlock()
}
func (r *receiver) received() (ids []string, verified []bool, bodies []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...), append([]bool(nil), r.verified...), append([]string(nil), r.bodies...)
}

type harness struct {
	db         *database.DB
	service    *outbound.Service
	queue      outbound.Queue
	job        outbound.DeliveryJob
	dispatcher *jobs.Dispatcher
	receiver   *receiver
}

func open(t *testing.T, attempts uint32) harness {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	registry, err := migrate.New(append(outbound.Migrations(), outbox.Migrations()...)...)
	if err != nil {
		t.Fatal(err)
	}
	migration := migrate.DefaultPostgresConfig()
	migration.Schema = scope.Schema()
	runner, err := migrate.NewPostgres(db, registry, migration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	h := harness{db: db, receiver: &receiver{}}
	h.receiver.server = httptest.NewServer(h.receiver)
	t.Cleanup(h.receiver.server.Close)
	address, _ := url.Parse(h.receiver.server.URL)
	port, _ := strconv.Atoi(address.Port())
	clientConfig := httpclient.DefaultConfig("webhooks")
	clientConfig.Destination = httpclient.DestinationPolicy{Mode: httpclient.RestrictedDestinations, Schemes: []httpclient.Scheme{httpclient.HTTP}, Ports: []uint16{uint16(port)}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	client, err := httpclient.New(clientConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	key, err := encryption.GenerateKey("webhooks")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring("webhooks", key)
	if err != nil {
		t.Fatal(err)
	}
	config := outbound.DefaultConfig()
	config.Schema = scope.Schema()
	h.service, err = outbound.New(outbound.Dependencies{DB: db, Keys: keys, Client: client, Clock: clock.System{}}, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.service.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	policy := jobs.DefaultPolicy("webhooks")
	policy.Attempts, policy.Backoff, policy.Jitter = attempts, []time.Duration{time.Millisecond}, 0
	h.job = outbound.DefineDeliveryJob("webhooks.deliver", policy)
	declaration, err := h.job.Declare(h.service)
	if err != nil {
		t.Fatal(err)
	}
	jobRegistry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := inline.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	h.dispatcher, err = jobs.NewDispatcher(backend, jobRegistry, jobs.DefaultDispatchConfig(testkit.Namespace(t)))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("webhooks", h.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	h.queue, err = h.job.ToOutbox(h.service, producer)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func (h harness) write(t *testing.T, fn func(context.Context, *database.Tx) error) error {
	t.Helper()
	return h.db.Transaction(t.Context(), func(tx *database.Tx) error { return fn(t.Context(), tx) }, database.TxOptions{Isolation: database.ReadCommitted})
}
func (h harness) send(t *testing.T, endpoint outbound.EndpointID, invoice string) outbound.DeliveryID {
	t.Helper()
	var id outbound.DeliveryID
	err := h.write(t, func(ctx context.Context, tx *database.Tx) error {
		var err error
		id, err = outbound.Send(ctx, tx, h.queue, endpoint, invoicePaid, InvoicePaid{Invoice: invoice})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// deliver executes the enqueued job as a worker would, through the inline
// driver, then runs retries that became due.
func (h harness) deliver(t *testing.T, id outbound.DeliveryID, rounds int) {
	t.Helper()
	if _, err := h.job.Definition().Dispatch(t.Context(), h.dispatcher, outbound.DeliveryRequest{Delivery: id}, jobs.Options[outbound.DeliveryRequest]{}); err != nil {
		t.Fatal(err)
	}
	h.retry(t, rounds)
}
func (h harness) retry(t *testing.T, rounds int) {
	t.Helper()
	for range rounds {
		time.Sleep(10 * time.Millisecond)
		if _, err := h.dispatcher.RunPending(t.Context(), "webhooks"); err != nil {
			t.Fatal(err)
		}
	}
}
func (h harness) delivery(t *testing.T, id outbound.DeliveryID) outbound.DeliveryInfo {
	t.Helper()
	rows, err := h.service.Deliveries(t.Context(), outbound.DeliveryQuery{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatal("delivery not logged")
	return outbound.DeliveryInfo{}
}

func TestPostgresOutboundDeliverySignsRetriesAndStaysIdempotent(t *testing.T) {
	h := open(t, 5)
	endpoint, signing, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/hooks", Events: []outbound.EventType{"invoice.paid"}})
	if err != nil || !strings.HasPrefix(signing.Reveal(), "whsec_") {
		t.Fatal(err)
	}
	h.receiver.trust(t, signing)
	id := h.send(t, endpoint, "inv_1")
	var queued int
	if err := h.write(t, func(ctx context.Context, tx *database.Tx) error {
		return database.ScanOne(ctx, tx, `SELECT count(*) FROM foundry_outbox`, nil, &queued)
	}); err != nil || queued != 1 {
		t.Fatal("send did not enqueue its delivery job in the same transaction", err, queued)
	}
	if row := h.delivery(t, id); row.State != outbound.Pending || row.Attempts != 0 || row.Event != "invoice.paid" {
		t.Fatal("delivery log entry", row)
	}
	h.receiver.script(503, 204)
	h.deliver(t, id, 0)
	if row := h.delivery(t, id); row.State != outbound.Pending || row.Attempts != 1 || row.LastStatus != 503 || row.LastFailure != "status_retryable" {
		t.Fatal("retryable failure was not logged", row)
	}
	h.retry(t, 1)
	row := h.delivery(t, id)
	if _, delivered := row.DeliveredAt.Get(); row.State != outbound.Succeeded || row.LastStatus != 204 || row.LastFailure != "" || !delivered {
		t.Fatal("retry did not succeed", row)
	}
	ids, verified, bodies := h.receiver.received()
	if len(ids) != 2 || ids[0] != outbound.WebhookID(id) || ids[1] != ids[0] || !verified[0] || !verified[1] {
		t.Fatal("receiver did not verify stable signed deliveries", ids, verified)
	}
	var envelope struct {
		Type      string          `json:"type"`
		Timestamp time.Time       `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &envelope); err != nil || envelope.Type != "invoice.paid" || string(envelope.Data) != `{"invoice":"inv_1"}` || envelope.Timestamp.IsZero() {
		t.Fatal("payload envelope changed", bodies[0], err)
	}
	// A succeeded delivery is never resent by a duplicate job.
	h.deliver(t, id, 0)
	if ids, _, _ := h.receiver.received(); len(ids) != 2 {
		t.Fatal("duplicate job resent a succeeded delivery")
	}
}

func TestPostgresOutboundRejectionReplayRotationAndExhaustion(t *testing.T) {
	h := open(t, 2)
	endpoint, first, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/hooks"})
	if err != nil {
		t.Fatal(err)
	}
	h.receiver.trust(t, first)
	rejected := h.send(t, endpoint, "inv_2")
	h.receiver.script(410)
	h.deliver(t, rejected, 1)
	if row := h.delivery(t, rejected); row.State != outbound.Failed || row.Attempts != 1 || row.LastStatus != 410 || row.LastFailure != "status_rejected" {
		t.Fatal("permanent rejection was retried or not logged", row)
	}
	if err := h.queue.Replay(t.Context(), rejected); err != nil {
		t.Fatal(err)
	}
	h.receiver.script(200)
	h.deliver(t, rejected, 0)
	if row := h.delivery(t, rejected); row.State != outbound.Succeeded || row.Attempts != 2 {
		t.Fatal("replay did not redeliver", row)
	}
	if ids, _, _ := h.receiver.received(); len(ids) != 2 || ids[0] != ids[1] {
		t.Fatal("replay changed the webhook-id", ids)
	}

	// Rotation signs with both secrets during the grace period.
	// The default rotation keeps the previous secret for Config.SecretGrace.
	second, err := h.service.RotateSecret(t.Context(), endpoint)
	if err != nil || second.Reveal() == first.Reveal() {
		t.Fatal(err)
	}
	for _, trusted := range []secret.String{first, second} {
		h.receiver.trust(t, trusted)
		h.deliver(t, h.send(t, endpoint, "inv_rotation"), 0)
		if _, verified, _ := h.receiver.received(); !verified[len(verified)-1] {
			t.Fatal("rotation broke verification with one of the secrets")
		}
	}
	third, err := h.service.RotateSecretWithGrace(t.Context(), endpoint, 0)
	if err != nil {
		t.Fatal(err)
	}
	h.receiver.trust(t, second)
	h.deliver(t, h.send(t, endpoint, "inv_revoked"), 0)
	if _, verified, _ := h.receiver.received(); verified[len(verified)-1] {
		t.Fatal("revoked secret still signed")
	}
	h.receiver.trust(t, third)

	// Retryable attempts stay pending; only the job runtime's terminal
	// failure (reported to the failure sink) records the delivery as failed.
	exhausted := h.send(t, endpoint, "inv_3")
	h.receiver.script(500, 500, 500)
	h.deliver(t, exhausted, 3)
	if row := h.delivery(t, exhausted); row.State != outbound.Pending || row.Attempts != 2 || row.LastStatus != 500 || row.LastFailure != "status_retryable" {
		t.Fatal("handler re-derived attempt exhaustion", row)
	}
	sink, err := h.job.FailureSink(h.service)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := h.job.Definition().Capture(t.Context(), outbound.DeliveryRequest{Delivery: exhausted}, jobs.Options[outbound.DeliveryRequest]{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.RecordFailure(t.Context(), jobs.FailedJob{Queue: "webhooks", Envelope: pending.Envelope(), Reason: jobs.AttemptLimit, Attempts: 2}); err != nil {
		t.Fatal(err)
	}
	if row := h.delivery(t, exhausted); row.State != outbound.Failed || row.Attempts != 2 || row.LastFailure != "status_retryable" {
		t.Fatal("terminal job failure was not recorded", row)
	}
	count, err := h.queue.ReplayFailed(t.Context(), value.Set(endpoint), 10)
	if err != nil || count != 1 {
		t.Fatal("failed deliveries were not replayed", err, count)
	}
}

func TestPostgresOutboundRegistryFanOutAndTransactionality(t *testing.T) {
	h := open(t, 3)
	subscribed := []outbound.EventType{"invoice.paid"}
	a, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/a", Events: subscribed})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/b", Events: subscribed}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/c", Events: []outbound.EventType{"invoice.voided"}}); err != nil {
		t.Fatal(err)
	}
	for _, options := range []outbound.EndpointOptions{
		{URL: "http://169.254.169.254/latest"},
		{URL: "https://example.com/hooks"},
		{URL: h.receiver.server.URL + "/d", Events: []outbound.EventType{"Bad Event"}},
		{URL: h.receiver.server.URL + "/d", Events: []outbound.EventType{"invoice.paid", "invoice.paid"}},
	} {
		if _, _, err := h.service.CreateEndpoint(t.Context(), options); err == nil {
			t.Fatal("endpoint outside the destination policy or with invalid events registered", options.URL)
		}
	}
	var ids []outbound.DeliveryID
	err = h.write(t, func(ctx context.Context, tx *database.Tx) error {
		var err error
		ids, err = outbound.Publish(ctx, tx, h.queue, invoicePaid, InvoicePaid{Invoice: "inv_fan"})
		return err
	})
	if err != nil || len(ids) != 2 {
		t.Fatal("fan-out did not reach exactly the subscribed endpoints", err, len(ids))
	}
	// A rolled-back business transaction logs and enqueues nothing.
	rollback := errors.New("business rollback")
	err = h.write(t, func(ctx context.Context, tx *database.Tx) error {
		if _, err := outbound.Send(ctx, tx, h.queue, a, invoicePaid, InvoicePaid{Invoice: "inv_rollback"}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	rows, err := h.service.Deliveries(t.Context(), outbound.DeliveryQuery{Limit: 100})
	if err != nil || len(rows) != 2 {
		t.Fatal("rolled-back send was logged", err, len(rows))
	}
	// Deactivation stops new sends and fails pending deliveries permanently.
	owned, err := h.service.Deliveries(t.Context(), outbound.DeliveryQuery{Endpoint: value.Set(a), Limit: 10})
	if err != nil || len(owned) != 1 {
		t.Fatal("endpoint delivery filter", err, len(owned))
	}
	pending := owned[0].ID
	info, err := h.service.UpdateEndpoint(t.Context(), a, outbound.EndpointOptions{URL: h.receiver.server.URL + "/a", Events: subscribed}, false)
	if err != nil || info.Active {
		t.Fatal(err)
	}
	err = h.write(t, func(ctx context.Context, tx *database.Tx) error {
		_, err := outbound.Send(ctx, tx, h.queue, a, invoicePaid, InvoicePaid{Invoice: "inv_inactive"})
		return err
	})
	if !errors.Is(err, fault.Conflict) {
		t.Fatal("inactive endpoint accepted a delivery", err)
	}
	h.deliver(t, pending, 1)
	if row := h.delivery(t, pending); row.State != outbound.Failed || row.LastFailure != "endpoint_inactive" {
		t.Fatal("pending delivery to an inactive endpoint", row)
	}
	endpoints, err := h.service.Endpoints(t.Context(), outbound.EndpointID{}, 10)
	if err != nil || len(endpoints) != 3 {
		t.Fatal("endpoint listing", err, len(endpoints))
	}
	// The operational command lists metadata only and never payloads.
	listing, err := command.Parse([]string{"webhooks", "deliveries", "--state", "failed", "--format", "json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := listing.Run(t.Context(), h.queue, &out); err != nil || !strings.Contains(out.String(), pending.String()) || !strings.Contains(out.String(), "endpoint_inactive") || strings.Contains(out.String(), "inv_fan") {
		t.Fatal("delivery listing", err, out.String())
	}
	if _, err := h.service.UpdateEndpoint(t.Context(), a, outbound.EndpointOptions{URL: h.receiver.server.URL + "/a", Events: subscribed}, true); err != nil {
		t.Fatal(err)
	}
	replay, err := command.Parse([]string{"webhooks", "replay", "--failed", "--endpoint", a.String()}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := replay.Run(t.Context(), h.queue, &out); err != nil || out.String() != "replayed=1\n" {
		t.Fatal("failed replay command", err, out.String())
	}
}
