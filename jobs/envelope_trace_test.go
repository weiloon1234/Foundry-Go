package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestEnvelopeUsesCurrentFormatWithOptionalTrace(t *testing.T) {
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
	definition := jobs.Define[payload]("envelope", 1, jobs.DefaultPolicy("default"))
	var steps []jobs.Step
	for _, propagate := range []bool{false, true} {
		pending, err := definition.Capture(ctx, payload{Labels: map[string]string{"private": "PAYLOAD_CANARY"}}, jobs.Options[payload]{PropagateTrace: propagate})
		if err != nil {
			t.Fatal(err)
		}
		data, err := pending.Envelope().MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"envelope_version":3`) {
			t.Fatal("writer omitted current format")
		}
		restored, err := jobs.DecodeEnvelope(data)
		if err != nil || restored.WireVersion() != jobs.CurrentEnvelopeVersion || restored.PayloadJSON() != pending.Envelope().PayloadJSON() {
			t.Fatal("current envelope did not round-trip", err)
		}
		captured, present := restored.Trace().Get()
		if present != propagate {
			t.Fatal("trace presence changed")
		}
		if present && (captured.TraceParent() != trace.TraceParent() || captured.TraceState() != trace.TraceState()) {
			t.Fatal("trace snapshot changed")
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"", "0", "1", "2", "4", "null"} {
			if version == "" {
				delete(wire, "envelope_version")
			} else {
				wire["envelope_version"] = json.RawMessage(version)
			}
			invalid, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := jobs.DecodeEnvelope(invalid); !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "PAYLOAD_CANARY") || strings.Contains(err.Error(), "vendor=private") {
				t.Fatal("unsupported format must fail with a safe invalid-input error", err)
			}
		}
		wire["envelope_version"] = json.RawMessage("3")
		for _, invalidTrace := range []string{"null", `{}`, `{"traceparent":"private-invalid"}`} {
			wire["trace"] = json.RawMessage(invalidTrace)
			invalid, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := jobs.DecodeEnvelope(invalid); err == nil {
				t.Fatal("invalid trace accepted")
			}
		}
		steps = append(steps, pending.Step())
	}
	workflow, err := jobs.NewChain(steps...)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := workflow.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := jobs.DecodeWorkflow(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range restored.Steps() {
		if step.WireVersion() != jobs.CurrentEnvelopeVersion {
			t.Fatal("workflow changed envelope format")
		}
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

func TestRedispatchKeepsCurrentFormatAfterClearingRetryDeadline(t *testing.T) {
	for _, propagate := range []bool{false, true} {
		f := newWorkerFixture(t, jobs.DefaultPolicy("default"), func(context.Context, payload) error { return nil })
		trace, err := tracing.New(true)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err := tracing.WithContext(t.Context(), trace)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := f.definition.Capture(ctx, payload{Labels: map[string]string{}}, jobs.Options[payload]{RetryUntil: time.Now().Add(time.Hour), PropagateTrace: propagate})
		if err != nil {
			t.Fatal(err)
		}
		id, err := f.dispatcher.Redispatch(ctx, pending.Envelope())
		if err != nil {
			t.Fatal(err)
		}
		record, err := f.backend.JobInspect(ctx, f.key, id)
		if err != nil {
			t.Fatal(err)
		}
		restored, ok := record.Get()
		if !ok || id == pending.Envelope().ID() || restored.Envelope.WireVersion() != jobs.CurrentEnvelopeVersion || !restored.Envelope.Policy().RetryUntil.IsZero() || restored.Envelope.Trace().IsSet() != propagate {
			t.Fatal("redispatch changed format or retained its old retry deadline")
		}
		data, err := restored.Envelope.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"envelope_version":3`) {
			t.Fatal("redispatch omitted current format")
		}
	}
}
