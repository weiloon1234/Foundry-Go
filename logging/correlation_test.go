package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestStructuredContextCorrelationNeverCapturesVendorState(t *testing.T) {
	trace, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	trace, err = trace.WithState("vendor=private-vendor-value")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracing.WithContext(t.Context(), trace)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: "request-123"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = attribution.WithContext(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := logging.JSON(&output, logging.Options{}).With("provider", "example").WithGroup("operation")
	logger.InfoContext(ctx, "observed", slog.Any("trace", trace), slog.String("tracestate", "private-vendor-value"))
	logger.InfoContext(context.Background(), "unrelated")
	if strings.Contains(output.String(), "private-vendor-value") {
		t.Fatal("vendor state reached structured logging")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatal("invalid JSON log records")
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	operation := first["operation"].(map[string]any)
	correlation := operation["correlation"].(map[string]any)
	if correlation["request_id"] != "request-123" || correlation["trace_id"] != trace.TraceID().String() || correlation["span_id"] != trace.SpanID().String() {
		t.Fatal("structured logger lost context correlation")
	}
	if strings.Contains(lines[1], "request-123") || strings.Contains(lines[1], trace.TraceID().String()) {
		t.Fatal("logger retained context across calls")
	}
}
