package tracing_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestTraceParentParsingAndLocalChildPolicy(t *testing.T) {
	remote, err := tracing.Parse(parent, " vendor=opaque,1tenant@system=value ")
	if err != nil || !remote.Sampled() || remote.TraceState() != "vendor=opaque,1tenant@system=value" || remote.TraceParent() != parent {
		t.Fatal("valid W3C context rejected", err)
	}
	ctx, err := tracing.WithContext(t.Context(), remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx, child, err := tracing.Start(ctx, false)
	if err != nil || child.TraceID() != remote.TraceID() || child.SpanID() == remote.SpanID() || child.Sampled() || child.TraceState() != remote.TraceState() || tracing.FromContext(ctx) != child {
		t.Fatal("child lost trace identity or local sampling policy", err)
	}
	future, err := tracing.Parse("0a"+parent[2:]+"-future-fields", "")
	if err != nil || future.TraceParent() != parent {
		t.Fatal("future-version prefix was not normalized", err)
	}
	flags, err := tracing.Parse(parent[:53]+"09", "")
	if err != nil || !flags.Sampled() || flags.TraceParent() != parent {
		t.Fatal("sampled flag was not masked", err)
	}
	ignored, err := tracing.Parse(parent, "duplicate=x,duplicate=y")
	if err != nil || ignored.TraceState() != "" || ignored.TraceID() != remote.TraceID() {
		t.Fatal("bad tracestate destroyed valid parent", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", remote, remote), "opaque") {
		t.Fatal("routine formatting leaked vendor metadata")
	}
}

func TestTraceContextRejectsMalformedParentsAndStrictStoredDocuments(t *testing.T) {
	for _, invalid := range []string{
		"", parent[:54], parent + "-extra", "ff" + parent[2:], strings.ToUpper(parent),
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"01" + parent[2:] + "unexpected", strings.Repeat("x", tracing.MaxParentBytes+1),
	} {
		if got, err := tracing.Parse(invalid, "secret=value"); !errors.Is(err, fault.Invalid) || !got.IsZero() {
			t.Fatal("malformed traceparent accepted", err)
		}
	}
	original, err := tracing.Parse(parent, "vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded tracing.Context
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != original {
		t.Fatal("trace snapshot did not round-trip", err)
	}
	for _, invalid := range []string{
		`null`, `{}`, `{"traceparent":"` + parent + `","unknown":1}`,
		`{"traceparent":"` + parent + `","traceparent":"` + parent + `"}`,
		`{"traceparent":"` + parent + `","tracestate":"bad=one,bad=two"}`,
	} {
		decoded = original
		if err := json.Unmarshal([]byte(invalid), &decoded); err == nil || decoded != original {
			t.Fatal("bad document changed receiver", err)
		}
	}
}

func TestTraceStateBoundsAndRootOwnership(t *testing.T) {
	root, err := tracing.New(true)
	if err != nil || root.TraceID().IsZero() || root.SpanID().IsZero() {
		t.Fatal("invalid random root", err)
	}
	for _, state := range []string{"Upper=value", "0simple=value", "vendor=", "vendor=a=b", "vendor=line\nbreak", "tenant@toolongtoolongx=value", "vendor=" + strings.Repeat("x", 257), strings.Repeat(" ,", 33), strings.Repeat("x", tracing.MaxStateBytes+1)} {
		if _, err := root.WithState(state); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid tracestate accepted", err)
		}
	}
	for _, state := range []string{"", " \t, ,vendor=ok,", "a@b=value", "0tenant@b=value", "vendor=leading space allowed"} {
		if _, err := root.WithState(state); err != nil {
			t.Fatal("valid tracestate rejected", err)
		}
	}
	// Cancellation belongs to the operation; a cancelled context still receives
	// trace identity so its observation is recorded rather than dropped.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	started, child, err := tracing.Start(ctx, true)
	if err != nil || child.Validate() != nil || tracing.FromContext(started) != child || !errors.Is(started.Err(), context.Canceled) {
		t.Fatal("cancelled trace start lost identity or cancellation", err)
	}
	if _, _, err := tracing.StartSampled(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil sampler accepted", err)
	}
	if _, _, err := tracing.Start(nil, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil trace start accepted", err)
	}
}

func FuzzTraceContext(f *testing.F) {
	f.Add(parent, "vendor=value")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, header, state string) {
		parsed, err := tracing.Parse(header, state)
		if err != nil {
			if !parsed.IsZero() {
				t.Fatal("failed parse retained metadata")
			}
			return
		}
		if parsed.Validate() != nil || len(parsed.TraceParent()) != 55 || len(parsed.TraceState()) > tracing.MaxStateBytes {
			t.Fatal("parsed trace violated bounds")
		}
		again, err := tracing.Parse(parsed.TraceParent(), parsed.TraceState())
		if err != nil || again != parsed {
			t.Fatal("normalized trace was not stable", err)
		}
	})
}

func TestRatioSamplerIsDeterministicByTrace(t *testing.T) {
	for _, invalid := range []float64{-0.1, 1.5} {
		if _, err := tracing.RatioSampler(invalid); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid ratio accepted", invalid)
		}
	}
	never, _ := tracing.RatioSampler(0)
	always, _ := tracing.RatioSampler(1)
	half, err := tracing.RatioSampler(0.5)
	if err != nil {
		t.Fatal(err)
	}
	sampled := 0
	for range 2000 {
		ctx, root, err := tracing.StartSampled(t.Context(), half)
		if err != nil {
			t.Fatal(err)
		}
		_, child, err := tracing.StartSampled(ctx, half)
		if err != nil || child.TraceID() != root.TraceID() || child.Sampled() != root.Sampled() {
			t.Fatal("spans of one trace disagreed on sampling", err)
		}
		if never(root.TraceID()) || !always(root.TraceID()) {
			t.Fatal("boundary ratios are not constant")
		}
		if root.Sampled() {
			sampled++
		}
	}
	if sampled < 800 || sampled > 1200 {
		t.Fatal("ratio sampler deviates from its fraction", sampled)
	}
}
