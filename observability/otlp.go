package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// OTLPHeader is one explicitly configured export header, typically collector
// authentication. Values stay redacted in formatting and logs.
type OTLPHeader struct {
	Name  string
	Value secret.String
}

// OTLPConfig configures the standard-library OTLP/HTTP JSON trace exporter.
// Endpoint is the complete traces URL, for example
// https://collector.example:4318/v1/traces. Timeout caps one request (zero
// selects 10 seconds); the recorder's TraceTimeout also bounds each call.
// A nil Client uses an owned client that never follows redirects, so
// configured headers are sent only to Endpoint.
type OTLPConfig struct {
	Endpoint    string
	ServiceName string
	Headers     []OTLPHeader
	Timeout     time.Duration
	Client      *http.Client
}

const otlpScope = "github.com/weiloon1234/Foundry-Go/observability"
const maxOTLPResponseBytes = 64 << 10

func (c OTLPConfig) Validate() error {
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil || endpoint.Scheme != "http" && endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || len(c.Endpoint) > 2048 {
		return fault.New(fault.Invalid, "OTLP endpoint must be an absolute HTTP(S) URL without credentials")
	}
	if c.ServiceName == "" || len(c.ServiceName) > 256 || strings.ContainsAny(c.ServiceName, "\r\n\x00") {
		return fault.New(fault.Invalid, "OTLP export requires a bounded service name")
	}
	if c.Timeout < 0 || c.Timeout > time.Minute || len(c.Headers) > 32 {
		return fault.New(fault.Invalid, "invalid OTLP timeout or header count")
	}
	seen := make(map[string]bool, len(c.Headers))
	for _, header := range c.Headers {
		name := http.CanonicalHeaderKey(header.Name)
		if !headerToken(header.Name) || seen[name] || strings.ContainsAny(header.Value.Reveal(), "\r\n\x00") || len(header.Value.Reveal()) > 8192 {
			return fault.New(fault.Invalid, "invalid or duplicate OTLP header")
		}
		switch name {
		case "Host", "Content-Type", "Content-Length", "Transfer-Encoding", "Connection":
			return fault.New(fault.Invalid, "OTLP header is owned by the transport")
		}
		seen[name] = true
	}
	return nil
}

func headerToken(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0) {
			return false
		}
	}
	return true
}

type otlpValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"`
}
type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}
type otlpStatus struct {
	Code int `json:"code,omitempty"`
}
type otlpSpan struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId,omitempty"`
	Name         string          `json:"name"`
	Kind         int             `json:"kind"`
	Start        string          `json:"startTimeUnixNano"`
	End          string          `json:"endTimeUnixNano"`
	Attributes   []otlpAttribute `json:"attributes"`
	Status       otlpStatus      `json:"status"`
}
type otlpScopeName struct {
	Name string `json:"name"`
}
type otlpScopeSpans struct {
	Scope otlpScopeName `json:"scope"`
	Spans []otlpSpan    `json:"spans"`
}
type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}
type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}
type otlpRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

func stringAttribute(key, value string) otlpAttribute {
	return otlpAttribute{Key: key, Value: otlpValue{StringValue: &value}}
}

// OTLP span kinds: internal, server, client and consumer.
func otlpKind(kind Kind) int {
	switch kind {
	case HTTP, Socket, SocketMessage:
		return 2
	case OutboundHTTP:
		return 3
	case Job:
		return 5
	default:
		return 1
	}
}

func otlpSpanFor(entry Entry) otlpSpan {
	span := otlpSpan{TraceID: entry.TraceID.String(), SpanID: entry.SpanID.String(), Name: string(entry.Operation.Kind) + " " + string(entry.Operation.Name), Kind: otlpKind(entry.Operation.Kind),
		Start: strconv.FormatInt(entry.Started.UnixNano(), 10), End: strconv.FormatInt(entry.Started.Add(entry.Duration).UnixNano(), 10),
		Attributes: []otlpAttribute{stringAttribute("foundry.operation.kind", string(entry.Operation.Kind)), stringAttribute("foundry.operation.name", string(entry.Operation.Name)), stringAttribute("foundry.outcome", string(entry.Result.Outcome))}}
	if !entry.ParentID.IsZero() {
		span.ParentSpanID = entry.ParentID.String()
	}
	if entry.Result.Status != 0 {
		status := strconv.Itoa(entry.Result.Status)
		span.Attributes = append(span.Attributes, otlpAttribute{Key: "http.response.status_code", Value: otlpValue{IntValue: &status}})
	}
	if entry.RequestID != "" {
		span.Attributes = append(span.Attributes, stringAttribute("foundry.request_id", string(entry.RequestID)))
	}
	switch entry.Result.Outcome {
	case Failed, Panicked, TimedOut:
		span.Status.Code = 2
	}
	return span
}

// NewOTLPExporter returns a batch exporter posting sampled spans as OTLP/HTTP
// JSON. Spans carry operation metadata, typed trace/span/request IDs and the
// outcome only; no payloads, error text or vendor tracestate. Each call makes
// one attempt without retry; a non-2xx response is an export failure.
func NewOTLPExporter(config OTLPConfig) (TraceBatchExporter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	headers := slices.Clone(config.Headers)
	endpoint, service := config.Endpoint, config.ServiceName
	return func(ctx context.Context, entries []Entry) error {
		if len(entries) == 0 {
			return nil
		}
		spans := make([]otlpSpan, 0, len(entries))
		for _, entry := range entries {
			spans = append(spans, otlpSpanFor(entry))
		}
		payload := otlpRequest{ResourceSpans: []otlpResourceSpans{{
			Resource:   otlpResource{Attributes: []otlpAttribute{stringAttribute("service.name", service)}},
			ScopeSpans: []otlpScopeSpans{{Scope: otlpScopeName{Name: otlpScope}, Spans: spans}},
		}}}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return fault.New(fault.Invalid, "cannot prepare OTLP export request")
		}
		request.Header.Set("Content-Type", "application/json")
		for _, header := range headers {
			request.Header.Set(header.Name, header.Value.Reveal())
		}
		response, err := client.Do(request)
		if err != nil {
			return fault.Wrap(fault.Internal, "OTLP export request failed", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxOTLPResponseBytes))
		closeErr := response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode > 299 {
			return fault.New(fault.Internal, "OTLP collector rejected the export with status "+strconv.Itoa(response.StatusCode))
		}
		return closeErr
	}, nil
}
