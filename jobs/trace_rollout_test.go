package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

// Frozen pre-milestone-24 wire reader. This compatibility fixture deliberately
// does not acquire newly added fields from the current envelope implementation.
type legacyEnvelopeReader struct {
	ID          jobs.ExecutionID   `json:"id"`
	Name        jobs.Name          `json:"name"`
	Version     jobs.Version       `json:"version"`
	Policy      jobs.Policy        `json:"policy"`
	AvailableAt time.Time          `json:"available_at"`
	Origin      attribution.Origin `json:"origin"`
	Unique      jobs.Uniqueness    `json:"unique"`
	Payload     json.RawMessage    `json:"payload"`
}

func decodeLegacyEnvelope(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope legacyEnvelopeReader
	return decoder.Decode(&envelope)
}

func TestJobTraceRequiresExplicitVersionedWorkersFirstRollout(t *testing.T) {
	trace, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	trace, err = trace.WithState("vendor=private")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracing.WithContext(t.Context(), trace)
	if err != nil {
		t.Fatal(err)
	}
	definition := jobs.Define[payload]("rollout", 1, jobs.DefaultPolicy("default"))
	legacy, err := definition.Capture(ctx, payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	legacyBytes, err := legacy.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if decodeLegacyEnvelope(legacyBytes) != nil || strings.Contains(string(legacyBytes), "envelope_version") || strings.Contains(string(legacyBytes), "trace") {
		t.Fatal("default producer broke older strict readers")
	}
	traced, err := definition.Capture(ctx, payload{Labels: map[string]string{}}, jobs.Options[payload]{PropagateTrace: true})
	if err != nil {
		t.Fatal(err)
	}
	tracedBytes, err := traced.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if decodeLegacyEnvelope(tracedBytes) == nil {
		t.Fatal("legacy reader incorrectly claimed traced-format compatibility")
	}
	for _, data := range [][]byte{legacyBytes, tracedBytes} {
		restored, err := jobs.DecodeEnvelope(data)
		if err != nil {
			t.Fatal(err)
		}
		if restored.Version() != 1 || restored.PayloadJSON() != legacy.Envelope().PayloadJSON() {
			t.Fatal("envelope rollout changed payload semantics")
		}
		if restored.WireVersion() == jobs.TracedEnvelope {
			captured, ok := restored.Trace().Get()
			if !ok || captured.TraceParent() != trace.TraceParent() || captured.TraceState() != trace.TraceState() {
				t.Fatal("trace snapshot did not survive transport")
			}
		} else if restored.Trace().IsSet() {
			t.Fatal("legacy message gained trace metadata")
		}
	}
	for _, data := range []string{
		strings.Replace(string(tracedBytes), `"envelope_version":2,`, "", 1),
		strings.Replace(string(tracedBytes), `"envelope_version":2`, `"envelope_version":3`, 1),
		strings.Replace(string(legacyBytes), `"id":`, `"envelope_version":2,"id":`, 1),
		strings.Replace(string(legacyBytes), `"id":`, `"trace":null,"id":`, 1),
	} {
		if _, err := jobs.DecodeEnvelope([]byte(data)); err == nil {
			t.Fatal("incoherent envelope extension accepted")
		}
	}
	workflow, err := jobs.NewChain(legacy.Step(), traced.Step())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := workflow.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.DecodeWorkflow(encoded); err != nil {
		t.Fatal("mixed old/new workflow was not readable", err)
	}
}

func TestTracedWorkerRestoresProducerParent(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := recorder.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	parent, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracing.WithContext(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan tracing.Context, 1)
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), func(ctx context.Context, _ payload) error { handled <- tracing.FromContext(ctx); return nil })
	receipt, err := f.definition.Dispatch(ctx, f.dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{PropagateTrace: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.worker.Run(observability.WithContext(t.Context(), recorder)) }()
	waitRecord(t, f, receipt.ID, jobs.Succeeded)
	if err := f.worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	trace := <-handled
	if trace.TraceID() != parent.TraceID() || trace.SpanID() == parent.SpanID() {
		t.Fatal("worker failed to derive producer child span")
	}
	entry := recorder.Snapshot().Recent[0]
	if entry.ParentID != parent.SpanID() || entry.TraceID != parent.TraceID() {
		t.Fatal("worker observation lost producer lineage")
	}
}
