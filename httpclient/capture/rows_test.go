package capture

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/capturedisclosure"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

type harness struct {
	recorder *httpcapture.Outbound
	config   Config
	client   *httpclient.Client
}

func testClientConfig(base string) httpclient.Config {
	config := httpclient.DefaultConfig("api")
	config.BaseURL = base
	config.Retry.InitialBackoff, config.Retry.MaxBackoff = 0, 0
	return config
}

// newHarness builds one captured client through httpclient.Module, as
// application assembly does, and redacts drained records like the writer.
func newHarness(t testing.TB, config Config, client httpclient.Config, transport http.RoundTripper, full ...httpclient.Name) harness {
	t.Helper()
	recorder, err := NewRecorder(config, full)
	if err != nil {
		t.Fatal(err)
	}
	contribution := foundation.Module{Name: "test.recorder", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Contribute(r, httpcapture.OutboundSinks, "recorder", func(foundation.Resolver) (*httpcapture.Outbound, error) { return recorder, nil })
	}}
	var adapter func(foundation.Resolver) (http.RoundTripper, error)
	if transport != nil {
		adapter = func(foundation.Resolver) (http.RoundTripper, error) { return transport, nil }
	}
	key := foundation.NewKey[*httpclient.Client]("test.client")
	app, err := foundation.NewBuilder().Register(contribution, httpclient.Module("test.client", key, client, nil, adapter)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
		recorder.Recorder().Seal()
		for record := recorder.Recorder().Drain(); record != nil; record = recorder.Recorder().Drain() {
			record.Release()
		}
	})
	return harness{recorder: recorder, config: config, client: c}
}

func (h harness) rows(t *testing.T) []store.Capture {
	t.Helper()
	var rows []store.Capture
	for record := h.recorder.Recorder().Drain(); record != nil; record = h.recorder.Recorder().Drain() {
		row, err := redact(record.Snapshot, record.DroppedBefore, h.config.policy())
		record.Release()
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}

func (h harness) row(t *testing.T) store.Capture {
	t.Helper()
	rows := h.rows(t)
	if len(rows) != 1 {
		t.Fatalf("want one capture row, got %d", len(rows))
	}
	return rows[0]
}

func omissionOf(row store.Capture, request bool) string {
	omission := row.ResponseBodyOmission
	if request {
		omission = row.RequestBodyOmission
	}
	text, _ := omission.Get()
	return text
}

func fieldValue(t *testing.T, fields []httpcapture.Field, name string) string {
	t.Helper()
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) && len(field.Values) > 0 {
			return field.Values[0]
		}
	}
	t.Fatalf("field %s missing", name)
	return ""
}

func TestRowsDiscloseOnlyWhatTheSharedRuleAllows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Set-Cookie", "session=abc")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "upstream-1")
		w.Header().Set("Content-Location", "/v1/receipts/1?X-Amz-Signature=presigned&part=1")
		_, _ = io.WriteString(w, `{"auth":"eyJ.session","author":"Ann","amount":12}`)
	}))
	t.Cleanup(server.Close)
	config := DefaultConfig()
	config.MaskNames = []string{"member_ref"}
	h := newHarness(t, config, testClientConfig(server.URL+"/v1"), nil)
	parent, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracing.WithContext(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	origin, _ := (attribution.Origin{}).WithRequest(attribution.Request{ID: "0192f3a4-5b6c-7d8e-9f01-23456789abcd"})
	ctx, _ = attribution.WithContext(ctx, origin)
	ctx, _ = attribution.WithRoute(ctx, attribution.Route{Method: "POST", Name: "webhooks.smartpay"})
	request := h.client.Post("merchant/auth").QueryPair("key", "google-key").QueryPair("page", "2").
		Bearer(secret.New("sk_live_123")).Header("Idempotency-Key", "idem-1").Header("Referer", "https://shop.test/checkout?member_ref=m1&step=2").
		Form(url.Values{"api_key": {"k"}, "member_ref": {"m1"}, "amount": {"12"}})
	if _, err := h.client.Do(ctx, request); err != nil {
		t.Fatal(err)
	}
	row := h.row(t)
	requestHeaders, _ := row.RequestHeaders.Decode()
	responseHeaders, _ := row.ResponseHeaders.Decode()
	query, _ := row.Query.Decode()
	if fieldValue(t, requestHeaders, "Authorization") != capturedisclosure.Masked || fieldValue(t, requestHeaders, "Idempotency-Key") != "idem-1" {
		t.Fatal("request credential header leaked or idempotency key hidden")
	}
	if fieldValue(t, responseHeaders, "Set-Cookie") != capturedisclosure.Masked || fieldValue(t, responseHeaders, "X-Request-Id") != "upstream-1" {
		t.Fatal("response session cookie leaked or ordinary header hidden")
	}
	if fieldValue(t, responseHeaders, "Content-Location") != "/v1/receipts/1?X-Amz-Signature=[masked]&part=1" || fieldValue(t, requestHeaders, "Referer") != "https://shop.test/checkout?member_ref=[masked]&step=2" {
		t.Fatal("signed or configured query value in a URL-valued header leaked")
	}
	if !row.RequestBodyComplete || row.RequestSentBytes != int64(len("amount=12&api_key=k&member_ref=m1")) {
		t.Fatal("a sent form body was not complete", row.RequestSentBytes)
	}
	if fieldValue(t, query, "key") != capturedisclosure.Masked || fieldValue(t, query, "page") != "2" {
		t.Fatal("credential query value leaked")
	}
	if string(row.RequestBody) != `[{"name":"amount","values":["12"]},{"name":"api_key","values":["[masked]"]},{"name":"member_ref","values":["[masked]"]}]` || row.RequestBodyState != string(BodyForm) {
		t.Fatal("form body was not redacted", string(row.RequestBody))
	}
	if string(row.ResponseBody) != `{"auth":"[masked]","author":"Ann","amount":12}` || row.ResponseBodyState != string(BodyJSON) {
		t.Fatal("gateway session token leaked", string(row.ResponseBody))
	}
	requestID, _ := row.RequestID.Get()
	traceID, _ := row.TraceID.Get()
	route, _ := row.Route.Get()
	if requestID != "0192f3a4-5b6c-7d8e-9f01-23456789abcd" || traceID != parent.TraceID().String() || route != "webhooks.smartpay" {
		t.Fatal("cause columns lost the inbound request", requestID, traceID, route)
	}
	if row.Result != string(ResultCompleted) || row.Attempt != 1 || row.BodyMode != string(Redacted) || row.Scheme != "http" || row.Path != "/v1/merchant/auth" || row.Method != "POST" {
		t.Fatal("call line changed", row.Result, row.Path)
	}
	if ip, _ := row.RemoteIP.Get(); ip != "127.0.0.1" {
		t.Fatal("remote address missing", ip)
	}
}

func TestFullModeDeclinedAndOpaqueBodies(t *testing.T) {
	large := bytes.Repeat([]byte("x"), 2048)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/v1/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>secret page</html>")
		case "/v1/gzip":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			writer := gzip.NewWriter(w)
			_, _ = io.WriteString(writer, `{"a":1}`)
			_ = writer.Close()
		case "/v1/large":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(large)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"t"}`)
		}
	}))
	t.Cleanup(server.Close)
	config := DefaultConfig()
	config.BodyBytes = 1024
	client := testClientConfig(server.URL + "/v1")
	client.Headers = http.Header{"Accept-Encoding": {"gzip"}}

	full := newHarness(t, config, client, nil, "api")
	body := []byte(`{"token":"t","amount":1}`)
	if _, err := full.client.Do(t.Context(), full.client.Post("exact").WithBody(httpclient.Bytes(body)).Header("Content-Type", "application/json")); err != nil {
		t.Fatal(err)
	}
	row := full.row(t)
	if row.BodyMode != string(Full) || row.RequestBodyState != string(BodyExact) || !bytes.Equal(row.RequestBody, body) || string(row.ResponseBody) != `{"token":"t"}` {
		t.Fatal("full mode did not store exact bodies", row.RequestBodyState, string(row.ResponseBody))
	}
	if _, err := full.client.Do(t.Context(), full.client.Post("large").WithBody(httpclient.Bytes(large))); err != nil {
		t.Fatal(err)
	}
	row = full.row(t)
	if row.RequestBodyState != string(BodyExactPrefix) || len(row.RequestBody) != 1024 || row.ResponseBodyState != string(BodyExactPrefix) || len(row.ResponseBody) != 1024 || row.ResponseReadBytes != 2048 {
		t.Fatal("full mode did not keep bounded prefixes", row.RequestBodyState, len(row.ResponseBody))
	}
	if _, err := full.client.Do(t.Context(), full.client.Post("exact").WithBody(httpclient.Bytes(body)).WithoutBodyCapture()); err != nil {
		t.Fatal(err)
	}
	row = full.row(t)
	if omissionOf(row, true) != string(OmittedDeclined) || omissionOf(row, false) != string(OmittedDeclined) || len(row.RequestBody)+len(row.ResponseBody) != 0 || row.RequestSentBytes != int64(len(body)) {
		t.Fatal("declined bodies were stored", omissionOf(row, true), omissionOf(row, false))
	}

	forced := config
	forced.ForceRedacted = true
	reduced := newHarness(t, forced, client, nil, "api")
	if _, err := reduced.client.Do(t.Context(), reduced.client.Get("html")); err != nil {
		t.Fatal(err)
	}
	if _, err := reduced.client.Do(t.Context(), reduced.client.Get("gzip")); err != nil {
		t.Fatal(err)
	}
	if _, err := reduced.client.Do(t.Context(), reduced.client.Post("large").WithBody(httpclient.Bytes(large)).Header("Content-Type", "application/json")); err != nil {
		t.Fatal(err)
	}
	rows := reduced.rows(t)
	if len(rows) != 3 || rows[0].BodyMode != string(Redacted) || omissionOf(rows[0], false) != string(OmittedUnsupportedMediaType) || omissionOf(rows[1], false) != string(OmittedContentEncoded) ||
		omissionOf(rows[2], true) != string(OmittedOverCaptureLimit) || omissionOf(rows[2], false) != string(OmittedOverCaptureLimit) {
		t.Fatal("force_redacted or opaque bodies changed", len(rows))
	}
	if rows[0].RequestBodyState != string(BodyEmpty) || rows[0].ResponseReadBytes != int64(len("<html>secret page</html>")) {
		t.Fatal("count-only response lost its byte count")
	}
}

func TestRowShapePerCallPath(t *testing.T) {
	var flaky atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flaky":
			if flaky.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"error":"busy"}`)
				return
			}
			w.WriteHeader(200)
		case "/v1/slow":
			time.Sleep(200 * time.Millisecond)
		case "/v1/missing":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":"missing"}`)
		case "/v1/stream":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[1,2,3]}`)
		case "/v1/huge":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "9999")
			_, _ = w.Write(make([]byte, 9999))
		case "/v1/none":
			w.WriteHeader(204)
		default:
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(server.Close)
	client := testClientConfig(server.URL + "/v1")
	client.ResponseBytes = 4096
	h := newHarness(t, DefaultConfig(), client, nil)
	c := h.client

	if _, err := c.Do(t.Context(), c.Get("flaky")); err != nil {
		t.Fatal(err)
	}
	rows := h.rows(t)
	if len(rows) != 2 || rows[0].OperationID != rows[1].OperationID || !rows[0].Retried || rows[1].Retried || omissionOf(rows[0], false) != "" ||
		string(rows[0].ResponseBody) != `{"error":"busy"}` || !rows[0].ResponseBodyComplete || rows[0].Result != string(ResultCompleted) {
		t.Fatal("retried status attempt lost its drained body or grouping")
	}

	short := c.Get("slow").WithRetry(httpclient.NoRetries())
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	_, err := c.Do(ctx, short)
	cancel()
	if err == nil {
		t.Fatal("deadline did not fail the call")
	}
	if row := h.row(t); row.Result != string(ResultTimedOut) || omissionOf(row, false) != string(OmittedNoResponse) || !row.Status.IsNull() || !row.HeadersMS.IsNull() {
		t.Fatal("timed out attempt changed", row.Result)
	}

	canceled, stop := context.WithCancel(t.Context())
	timer := time.AfterFunc(50*time.Millisecond, stop)
	defer timer.Stop()
	if _, err := c.Do(canceled, short); err == nil {
		t.Fatal("canceled call succeeded")
	}
	if row := h.row(t); row.Result != string(ResultCanceled) || row.Attempt != 1 {
		t.Fatal("canceled call stored the wrong result", row.Result)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	refused := newHarness(t, DefaultConfig(), testClientConfig(closed.URL), nil)
	if _, err := refused.client.Do(t.Context(), refused.client.Get("x").WithRetry(httpclient.NoRetries())); err == nil {
		t.Fatal("refused connection succeeded")
	}
	if row := refused.row(t); row.Result != string(ResultTransportFailed) || omissionOf(row, false) != string(OmittedNoResponse) || row.RequestBodyState != string(BodyEmpty) {
		t.Fatal("transport failure changed", row.Result)
	}
	offered := refused.client.Post("x").WithBody(httpclient.Bytes([]byte(`{"amount":1}`))).Header("Content-Type", "application/json").WithRetry(httpclient.NoRetries())
	if _, err := refused.client.Do(t.Context(), offered); err == nil {
		t.Fatal("refused connection succeeded")
	}
	if row := refused.row(t); row.RequestBodyComplete || row.RequestSentBytes != 0 || row.RequestBodyState != string(BodyJSON) || string(row.RequestBody) != `{"amount":1}` {
		t.Fatal("an unsent in-memory body claimed completeness or lost what was offered", row.RequestBodyComplete, row.RequestSentBytes, row.RequestBodyState)
	}

	if err := c.Stream(t.Context(), c.Get("stream"), func(_ context.Context, response *httpclient.StreamResponse) error {
		_, err := response.Read(make([]byte, 4))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if row := h.row(t); row.Result != string(ResultCompleted) || omissionOf(row, false) != string(OmittedIncomplete) || row.ResponseReadBytes != 4 || row.ResponseBodyComplete {
		t.Fatal("partial stream was stored as complete", omissionOf(row, false))
	}

	var sink bytes.Buffer
	if _, err := c.Download(t.Context(), c.Get("missing"), &sink, 1<<20); err == nil {
		t.Fatal("non-2xx download succeeded")
	}
	if row := h.row(t); row.Result != string(ResultCompleted) || omissionOf(row, false) != string(OmittedIncomplete) || row.ResponseReadBytes != 0 || sink.Len() != 0 {
		t.Fatal("non-2xx download read a body or failed its row", row.Result, row.ResponseReadBytes)
	}

	if _, err := c.Do(t.Context(), c.Get("huge")); err == nil {
		t.Fatal("oversized response succeeded")
	}
	if row := h.row(t); row.Result != string(ResultResponseTooLarge) || omissionOf(row, false) != string(OmittedIncomplete) || row.ResponseReadBytes != 0 {
		t.Fatal("Content-Length over the limit changed", row.Result)
	}

	for _, request := range []httpclient.Request{c.Request(http.MethodHead, "ok"), c.Get("none")} {
		if _, err := c.Do(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		if row := h.row(t); row.ResponseBodyState != string(BodyEmpty) || !row.ResponseBodyComplete {
			t.Fatal("HEAD or 204 body was not empty", row.ResponseBodyState)
		}
	}

	failing := httpclient.StreamBody(-1, func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(io.MultiReader(strings.NewReader(`{"a":`), failingReader{})), nil
	})
	if _, err := c.Do(t.Context(), c.Post("upload").WithBody(failing).Header("Content-Type", "application/json")); err == nil {
		t.Fatal("failed request stream succeeded")
	}
	if row := h.row(t); row.Result != string(ResultRequestBodyFailed) || omissionOf(row, true) != string(OmittedIncomplete) || row.RequestBodyComplete {
		t.Fatal("request stream failure changed", row.Result, omissionOf(row, true))
	}
	unopened := httpclient.StreamBody(4, func(context.Context) (io.ReadCloser, error) { return nil, errors.New("private open failure") })
	if _, err := c.Do(t.Context(), c.Post("upload").WithBody(unopened)); err == nil {
		t.Fatal("unopened request stream succeeded")
	}
	if row := h.row(t); row.Result != string(ResultRequestBodyFailed) || omissionOf(row, true) != string(OmittedIncomplete) || omissionOf(row, false) != string(OmittedNoResponse) {
		t.Fatal("unopened request stream was stored as empty", row.Result, row.RequestBodyState)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("private source failure") }

func TestNeverSentCallsAreAttemptZero(t *testing.T) {
	restricted := testClientConfig("")
	restricted.Destination = httpclient.PublicDestinations()
	denied := newHarness(t, DefaultConfig(), restricted, nil)
	if _, err := denied.client.Do(t.Context(), denied.client.Get("http://127.0.0.1/admin")); err == nil {
		t.Fatal("private destination was allowed")
	}
	row := denied.row(t)
	if row.Attempt != 0 || row.Result != string(ResultDestinationDenied) || omissionOf(row, true) != string(OmittedNotSent) || omissionOf(row, false) != string(OmittedNoResponse) || row.Retried {
		t.Fatal("URL destination denial changed", row.Attempt, row.Result)
	}
	if _, err := denied.client.Do(t.Context(), httpclient.Request{}); err == nil || len(denied.rows(t)) != 0 {
		t.Fatal("a request without a URL was captured")
	}

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	single := testClientConfig("https://upstream.test/v1")
	single.Concurrency = 1
	busy := newHarness(t, DefaultConfig(), single, roundTrip(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-release
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	}))
	done := make(chan error, 1)
	go func() { _, err := busy.client.Do(context.Background(), busy.client.Get("hold")); done <- err }()
	<-entered
	queued, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := busy.client.Do(queued, busy.client.Get("next")); err == nil {
		t.Fatal("saturated client admitted a call")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows := busy.rows(t)
	if len(rows) != 2 || rows[0].Result != string(ResultOverloaded) || rows[0].Attempt != 0 || omissionOf(rows[0], true) != string(OmittedNotSent) {
		t.Fatal("overloaded call was not stored as attempt 0", len(rows))
	}
}

func TestDialTimeDenialOnAttemptOneKeepsTheTappedBody(t *testing.T) {
	config := testClientConfig("")
	config.Destination = httpclient.DestinationPolicy{Mode: httpclient.RestrictedDestinations, Schemes: []httpclient.Scheme{httpclient.HTTP}, Ports: []uint16{80}}
	h := newHarness(t, DefaultConfig(), config, nil)
	if _, err := h.client.Do(t.Context(), h.client.Post("http://localhost/hook").WithBody(httpclient.Bytes([]byte(`{"a":1}`))).Header("Content-Type", "application/json")); err == nil {
		t.Fatal("dial to a private address was allowed")
	}
	row := h.row(t)
	if row.Attempt != 1 || row.Result != string(ResultDestinationDenied) || omissionOf(row, true) == string(OmittedNotSent) || omissionOf(row, false) != string(OmittedNoResponse) {
		t.Fatal("dial-time denial on attempt 1 changed", row.Attempt, row.Result, omissionOf(row, true))
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
