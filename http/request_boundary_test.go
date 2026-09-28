package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/model"
)

func requestBoundary(handler stdhttp.HandlerFunc, limit int64) stdhttp.Handler {
	c := DefaultServerConfig()
	c.MaxBodyBytes = limit
	return newHandlerLifetime().wrap(handler, slog.New(slog.NewTextHandler(io.Discard, nil)), c)
}

type countedBody struct {
	io.Reader
	read   int
	closed bool
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (b *countedBody) Close() error { b.closed = true; return nil }

func decodeFailure(t *testing.T, recorder *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var payload ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("error response was not valid JSON: %v", err)
	}
	if payload.Status != recorder.Code || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatal("error metadata disagrees with wire response")
	}
	if string(payload.RequestID) != recorder.Header().Get(RequestIDHeader) {
		t.Fatal("error response lost correlation with its header")
	}
	return payload
}

func TestDeclaredBodyLimitRejectsBeforeReading(t *testing.T) {
	body := &countedBody{Reader: strings.NewReader(strings.Repeat("x", 100))}
	request := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
	request.Body, request.ContentLength = body, 100
	request.Header.Set(RequestIDHeader, "forged-client-id")
	recorder := httptest.NewRecorder()
	requestBoundary(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		t.Fatal("oversized declaration reached the handler")
	}, 8).ServeHTTP(recorder, request)
	payload := decodeFailure(t, recorder)
	if payload.Code != PayloadTooLarge || payload.Status != 413 || body.read != 0 || payload.RequestID == "forged-client-id" {
		t.Fatalf("declared limit: %#v; read %d bytes", payload, body.read)
	}
	if _, err := model.ParseID[requestIdentity](string(payload.RequestID)); err != nil {
		t.Fatalf("request correlation does not reuse UUID generation: %v", err)
	}
}

func TestUnknownBodyLimitBoundsReadsAndReportsTypedCause(t *testing.T) {
	body := &countedBody{Reader: strings.NewReader(strings.Repeat("x", 100))}
	request := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
	request.Body, request.ContentLength = body, -1
	recorder := httptest.NewRecorder()
	requestBoundary(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		data, err := io.ReadAll(r.Body)
		var limit *stdhttp.MaxBytesError
		if !errors.As(err, &limit) || limit.Limit != 8 || len(data) != 8 {
			t.Fatalf("unknown-length body was not bounded: len=%d err=%v", len(data), err)
		}
		if err := WriteError(w, r, PayloadTooLarge.WithCause(err)); err != nil {
			t.Fatal(err)
		}
	}, 8).ServeHTTP(recorder, request)
	if payload := decodeFailure(t, recorder); payload.Code != PayloadTooLarge {
		t.Fatalf("limit error lost its classification: %#v", payload)
	}
	if body.read > 9 || !body.closed {
		t.Fatalf("limit reader ownership: read=%d closed=%t", body.read, body.closed)
	}
}

type applicationValue struct{}

func TestRequestAttributionIsFreshAndKeepsOrdinaryContextValues(t *testing.T) {
	origin, err := (attribution.Origin{}).WithSystem("fixture.worker")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(context.WithValue(t.Context(), applicationValue{}, "kept"), origin)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(ctx, stdhttp.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	request.Header.Set("Forwarded", "for=203.0.113.98")
	request.Header.Set("User-Agent", "fixture-client")
	request.Header.Set(RequestIDHeader, "spoofed")
	var previous attribution.RequestID
	for range 2 {
		recorder := httptest.NewRecorder()
		requestBoundary(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			attributed := attribution.FromContext(r.Context())
			metadata := attributed.Request()
			if attributed.System() != "" || attributed.Guard() != "" {
				t.Fatal("application identity leaked into a new HTTP request")
			}
			if metadata.ID == "" || metadata.ID == "spoofed" || metadata.ID == previous || RequestID(r.Context()) != metadata.ID {
				t.Fatal("request ID was missing, duplicated or trusted client input")
			}
			previous = metadata.ID
			if metadata.IP != netip.MustParseAddr("192.0.2.10") || metadata.UserAgent != "fixture-client" {
				t.Fatalf("request metadata did not use the direct peer: %#v", metadata)
			}
			if r.Context().Value(applicationValue{}) != "kept" {
				t.Fatal("request context lost an application value")
			}
			w.WriteHeader(stdhttp.StatusNoContent)
		}, 8).ServeHTTP(recorder, request)
		if recorder.Header().Get(RequestIDHeader) != string(previous) {
			t.Fatal("response header did not retain request attribution")
		}
	}
	if attribution.FromContext(request.Context()).System() != origin.System() {
		t.Fatal("request preparation mutated caller context attribution")
	}
}

func TestInvalidRequestMetadataRetainsCorrelation(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Set("User-Agent", strings.Repeat("x", attribution.MaxUserAgentBytes+1))
	recorder := httptest.NewRecorder()
	requestBoundary(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		t.Fatal("invalid request metadata reached handler")
	}, 8).ServeHTTP(recorder, request)
	payload := decodeFailure(t, recorder)
	if payload.Code != BadRequest || payload.RequestID == "" {
		t.Fatalf("invalid metadata response: %#v", payload)
	}
}
