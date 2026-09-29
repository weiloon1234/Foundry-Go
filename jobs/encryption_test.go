package jobs_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func keyring(t *testing.T) *encryption.Keyring {
	t.Helper()
	key, err := encryption.GenerateKey("jobs-2026")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := encryption.NewKeyring("jobs-2026", key)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func TestEncryptedPayloadsAreSealedAtCaptureAndOpenedByWorkers(t *testing.T) {
	ring := keyring(t)
	var received atomic.Value
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	f.definition = f.definition.Encrypted(ring)
	f = withWorker(t, f, func(_ context.Context, p payload) error { received.Store(p.Labels["card"]); return nil }, func(*jobs.WorkerConfig) {})
	pending, err := f.definition.Capture(t.Context(), payload{Labels: map[string]string{"card": "4242-private"}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	envelope := pending.Envelope()
	if !envelope.Encrypted() || envelope.WireVersion() != jobs.ExtendedEnvelope || strings.Contains(envelope.PayloadJSON(), "4242") {
		t.Fatal("payload was not sealed at capture")
	}
	data, err := envelope.MarshalJSON()
	if err != nil || strings.Contains(string(data), "4242") {
		t.Fatal("transport envelope exposed the payload", err)
	}
	decoded, err := f.definition.Payload(t.Context(), envelope)
	if err != nil || decoded.Labels["card"] != "4242-private" {
		t.Fatal("payload did not decrypt for tooling", err)
	}
	if _, err := jobs.Define[payload]("work", 1, jobs.DefaultPolicy("default")).Payload(t.Context(), envelope); err == nil {
		t.Fatal("a definition without the keyring decrypted the payload")
	}
	receipt, err := pending.Dispatch(t.Context(), f.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	runWorker(t, f)
	waitRecord(t, f, receipt.ID, jobs.Succeeded)
	if received.Load() != "4242-private" {
		t.Fatal("worker did not receive the decrypted payload")
	}
}

func TestWorkerWithoutKeyringRetriesEncryptedPayload(t *testing.T) {
	ring := keyring(t)
	policy := jobs.DefaultPolicy("default")
	policy.Attempts, policy.Backoff, policy.Jitter = 2, []time.Duration{time.Millisecond}, 0
	f := newWorkerFixture(t, policy, func(context.Context, payload) error { t.Error("handler ran without decryption"); return nil })
	pending, err := f.definition.Encrypted(ring).Capture(t.Context(), payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.backend.JobEnqueue(t.Context(), f.key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	runWorker(t, f)
	record := waitRecord(t, f, pending.ID(), jobs.Failed)
	if record.Attempts != 2 || record.History[len(record.History)-1].Reason == jobs.PayloadInvalid {
		t.Fatal("missing keyring should be retried, not treated as an invalid payload", record.Attempts)
	}
}
