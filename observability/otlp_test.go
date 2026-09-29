package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

type otlpDocument struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []struct {
				Key   string `json:"key"`
				Value struct {
					StringValue string `json:"stringValue"`
				} `json:"value"`
			} `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Spans []struct {
				TraceID      string `json:"traceId"`
				SpanID       string `json:"spanId"`
				ParentSpanID string `json:"parentSpanId"`
				Name         string `json:"name"`
				Kind         int    `json:"kind"`
				Start        string `json:"startTimeUnixNano"`
				End          string `json:"endTimeUnixNano"`
				Status       struct {
					Code int `json:"code"`
				} `json:"status"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

func TestOTLPExporterPostsBatchedJSONThroughRecorder(t *testing.T) {
	var mu sync.Mutex
	var documents []otlpDocument
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var document otlpDocument
		if r.Method != http.MethodPost || r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/json" || json.Unmarshal(body, &document) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		documents = append(documents, document)
		authorization = r.Header.Get("Authorization")
		mu.Unlock()
	}))
	defer server.Close()
	exporter, err := observability.NewOTLPExporter(observability.OTLPConfig{Endpoint: server.URL + "/v1/traces", ServiceName: "orders", Headers: []observability.OTLPHeader{{Name: "Authorization", Value: secret.New("Bearer collector-token")}}})
	if err != nil {
		t.Fatal(err)
	}
	config := observability.DefaultConfig()
	config.TraceBatchExporters = []observability.TraceBatchExporter{exporter}
	config.TraceBatchSize = 4
	recorder, err := observability.New(config)
	if err != nil {
		t.Fatal(err)
	}
	// Queue spans before the workers start so they drain as bounded batches.
	work, parent, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.HTTP, Name: "request"})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		_, child, err := recorder.Start(work, observability.Operation{Kind: observability.OutboundHTTP, Name: "billing"})
		if err != nil {
			t.Fatal(err)
		}
		child.End(observability.Result{Outcome: observability.Failed})
	}
	parent.End(observability.Result{Status: 200})
	done := make(chan error, 1)
	go func() { done <- recorder.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := recorder.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	spans, root := 0, tracing.FromContext(work)
	for _, document := range documents {
		batch := document.ResourceSpans[0].ScopeSpans[0].Spans
		if len(batch) > 4 || document.ResourceSpans[0].Resource.Attributes[0].Value.StringValue != "orders" {
			t.Fatal("batch exceeded its bound or lost its resource", len(batch))
		}
		for _, span := range batch {
			spans++
			if span.TraceID != root.TraceID().String() || span.Start == "" || span.End == "" {
				t.Fatal("span lost trace identity", span)
			}
			if span.Name == "outbound_http billing" && (span.Kind != 3 || span.Status.Code != 2 || span.ParentSpanID != root.SpanID().String()) {
				t.Fatal("client span kind, status or parent is wrong", span)
			}
			if span.Name == "http request" && (span.Kind != 2 || span.Status.Code != 0) {
				t.Fatal("server span kind or status is wrong", span)
			}
		}
	}
	if spans != 6 || len(documents) < 2 || authorization != "Bearer collector-token" {
		t.Fatal("exporter lost spans, batching or headers", spans, len(documents))
	}
	if recorder.Snapshot().TraceExportFailures != 0 {
		t.Fatal("successful exports were counted as failures")
	}
}

func TestOTLPExporterRejectsUnsafeConfigurationAndFailedExports(t *testing.T) {
	for _, config := range []observability.OTLPConfig{
		{Endpoint: "ftp://collector/v1/traces", ServiceName: "orders"},
		{Endpoint: "https://user:pass@collector/v1/traces", ServiceName: "orders"},
		{Endpoint: "https://collector/v1/traces"},
		{Endpoint: "https://collector/v1/traces", ServiceName: "orders", Headers: []observability.OTLPHeader{{Name: "Bad Header", Value: secret.New("x")}}},
		{Endpoint: "https://collector/v1/traces", ServiceName: "orders", Headers: []observability.OTLPHeader{{Name: "X-Token", Value: secret.New("a\r\nInjected: b")}}},
		{Endpoint: "https://collector/v1/traces", ServiceName: "orders", Headers: []observability.OTLPHeader{{Name: "Content-Type", Value: secret.New("text/plain")}}},
	} {
		if _, err := observability.NewOTLPExporter(config); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsafe OTLP configuration accepted", config.Endpoint, err)
		}
	}
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("private collector detail"))
	}))
	defer server.Close()
	entry := observability.Entry{Operation: observability.Operation{Kind: observability.Job, Name: "deliver"}, Result: observability.Result{Outcome: observability.Succeeded}, Started: time.Now()}
	for _, path := range []string{"/v1/traces", "/redirect"} {
		exporter, err := observability.NewOTLPExporter(observability.OTLPConfig{Endpoint: server.URL + path, ServiceName: "orders", Headers: []observability.OTLPHeader{{Name: "Authorization", Value: secret.New("Bearer secret")}}})
		if err != nil {
			t.Fatal(err)
		}
		err = exporter(t.Context(), []observability.Entry{entry})
		if err == nil || strings.Contains(err.Error(), "private collector detail") {
			t.Fatal("failed export accepted or leaked the response body", err)
		}
	}
	if redirected {
		t.Fatal("exporter followed a redirect with configured credentials")
	}
}
