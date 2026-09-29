package outbound_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound/command"
)

// A real worker decides finality (attempts, retry deadline, exception limit)
// and reports the terminal failure to the delivery job's failure sink.
func TestPostgresOutboundRuntimeFinalizesThroughFailureSink(t *testing.T) {
	h := open(t, 2)
	endpoint, signing, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/hooks"})
	if err != nil {
		t.Fatal(err)
	}
	h.receiver.trust(t, signing)
	h.receiver.script(500, 500, 500)
	id := h.send(t, endpoint, "inv_worker")
	declaration, err := h.job.Declare(h.service)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	namespace := testkit.Namespace(t)
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	sink, err := h.job.FailureSink(h.service)
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(namespace, "webhooks")
	config.Concurrency, config.PollInterval = 1, time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config, jobs.WithFailureSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	if _, err := h.job.Definition().Dispatch(t.Context(), dispatcher, outbound.DeliveryRequest{Delivery: id}, jobs.Options[outbound.DeliveryRequest]{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		row := h.delivery(t, id)
		if row.State == outbound.Failed {
			if row.Attempts != 2 || row.LastStatus != 500 || row.LastFailure != "status_retryable" {
				t.Fatal("terminal failure lost its last attempt", row)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exhausted delivery stayed pending", row)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPostgresOutboundReplayFailedSkipsInactiveEndpoints(t *testing.T) {
	h := open(t, 1)
	active, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/a"})
	if err != nil {
		t.Fatal(err)
	}
	inactive, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/b"})
	if err != nil {
		t.Fatal(err)
	}
	h.receiver.script(410, 410)
	kept, dropped := h.send(t, active, "inv_a"), h.send(t, inactive, "inv_b")
	h.deliver(t, kept, 0)
	h.deliver(t, dropped, 0)
	if _, err := h.service.UpdateEndpoint(t.Context(), inactive, outbound.EndpointOptions{URL: h.receiver.server.URL + "/b"}, false); err != nil {
		t.Fatal(err)
	}
	count, err := h.queue.ReplayFailed(t.Context(), value.Optional[outbound.EndpointID]{}, 10)
	if err != nil || count != 1 {
		t.Fatal("one inactive endpoint blocked replay of the others", err, count)
	}
	if h.delivery(t, kept).State != outbound.Pending || h.delivery(t, dropped).State != outbound.Failed {
		t.Fatal("replay touched the wrong deliveries")
	}
	if count, err := h.queue.ReplayFailed(t.Context(), value.Set(inactive), 10); !errors.Is(err, fault.Conflict) || count != 0 {
		t.Fatal("explicit inactive endpoint replay", err, count)
	}
}

func TestPostgresOutboundPreSendFailureIsLoggedThenFinalized(t *testing.T) {
	h := open(t, 3)
	endpoint, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/hooks"})
	if err != nil {
		t.Fatal(err)
	}
	id := h.send(t, endpoint, "inv_unsigned")
	// A service with a different keyring cannot open the stored secret.
	key, err := encryption.GenerateKey("other")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring("other", key)
	if err != nil {
		t.Fatal(err)
	}
	clientConfig := httpclient.DefaultConfig("other")
	clientConfig.Destination = httpclient.PublicDestinations()
	client, err := httpclient.New(clientConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	other, err := outbound.New(outbound.Dependencies{DB: h.db, Keys: keys, Client: client, Clock: clock.System{}}, h.service.Config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close(context.Background()) })
	if err := other.Deliver(t.Context(), id); err == nil {
		t.Fatal("delivery without an openable secret succeeded")
	}
	if row := h.delivery(t, id); row.State != outbound.Pending || row.Attempts != 1 || row.LastFailure != "signing_unavailable" {
		t.Fatal("pre-send failure was not logged", row)
	}
	if ids, _, _ := h.receiver.received(); len(ids) != 0 {
		t.Fatal("unsigned delivery was sent")
	}
	sink, err := h.job.FailureSink(h.service)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := h.job.Definition().Capture(t.Context(), outbound.DeliveryRequest{Delivery: id}, jobs.Options[outbound.DeliveryRequest]{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.RecordFailure(t.Context(), jobs.FailedJob{Queue: "webhooks", Envelope: pending.Envelope(), Reason: jobs.TimedOut}); err != nil {
		t.Fatal(err)
	}
	if row := h.delivery(t, id); row.State != outbound.Failed || row.LastFailure != "signing_unavailable" {
		t.Fatal("terminal pre-send failure stayed pending", row)
	}
}

func TestPostgresOutboundLargeSuccessfulResponseIsDelivered(t *testing.T) {
	h := open(t, 3)
	endpoint, signing, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/hooks"})
	if err != nil {
		t.Fatal(err)
	}
	h.receiver.trust(t, signing)
	h.receiver.mu.Lock()
	h.receiver.bodyBytes = 5 << 20 // beyond the client's default ResponseBytes
	h.receiver.mu.Unlock()
	h.receiver.script(200)
	id := h.send(t, endpoint, "inv_large")
	h.deliver(t, id, 1)
	if row := h.delivery(t, id); row.State != outbound.Succeeded || row.Attempts != 1 || row.LastStatus != 200 {
		t.Fatal("accepted delivery with a large response was retried or failed", row)
	}
	if ids, _, _ := h.receiver.received(); len(ids) != 1 {
		t.Fatal("accepted delivery was resent", len(ids))
	}
}

func TestPostgresOutboundPruneRemovesOnlyOldFinishedDeliveries(t *testing.T) {
	h := open(t, 3)
	endpoint, _, err := h.service.CreateEndpoint(t.Context(), outbound.EndpointOptions{URL: h.receiver.server.URL + "/hooks"})
	if err != nil {
		t.Fatal(err)
	}
	h.receiver.script(204, 410)
	succeeded, failed, pending := h.send(t, endpoint, "inv_ok"), h.send(t, endpoint, "inv_rejected"), h.send(t, endpoint, "inv_pending")
	h.deliver(t, succeeded, 0)
	h.deliver(t, failed, 0)
	if count, err := h.service.PruneDeliveries(t.Context(), time.Hour, 100); err != nil || count != 0 {
		t.Fatal("recent deliveries were pruned", err, count)
	}
	if err := h.write(t, func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_webhook_deliveries SET updated_at = updated_at - interval '3 hours'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	prune, err := command.Parse([]string{"webhooks", "prune", "--older-than", "2h", "--limit", "1"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := prune.Run(t.Context(), h.queue, &out); err != nil || out.String() != "pruned=2\n" {
		t.Fatal("prune command", err, out.String())
	}
	rows, err := h.service.Deliveries(t.Context(), outbound.DeliveryQuery{Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].ID != pending {
		t.Fatal("prune removed a pending delivery or kept finished ones", err, len(rows))
	}
	for _, invalid := range []struct {
		age   time.Duration
		limit int
	}{{time.Minute, 10}, {time.Hour, 0}, {time.Hour, outbound.MaxPruneBatch + 1}} {
		if _, err := h.service.PruneDeliveries(t.Context(), invalid.age, invalid.limit); err == nil {
			t.Fatal("unbounded prune accepted", invalid)
		}
	}
}
