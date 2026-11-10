package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

func captureTestRecorder(t testing.TB) *httpcapture.Recorder {
	t.Helper()
	c := httpcapture.DefaultConfig()
	c.BodyBytes, c.Rate, c.Burst = 1024, 10000, 10000
	r, err := httpcapture.New(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Seal()
		for record := r.Drain(); record != nil; record = r.Drain() {
			record.Release()
		}
	})
	return r
}

// This fake store only consumes the static recorder. No database, lifecycle
// provider or application wiring participates in kernel tests.
func captureTestTake(t *testing.T, recorder *httpcapture.Recorder) httpcapture.Snapshot {
	t.Helper()
	record := recorder.Drain()
	if record == nil {
		t.Fatal("missing capture")
	}
	t.Cleanup(record.Release)
	return record.Snapshot
}

func captureTestHandler(t testing.TB, next stdhttp.Handler, config ServerConfig, recorder *httpcapture.Recorder, observers ...RequestObserver) stdhttp.Handler {
	t.Helper()
	prefix, err := ParsePathPrefix("/wh")
	if err != nil {
		t.Fatal(err)
	}
	opts, err := configureServer([]ServerOption{WithWebhookCapture(recorder, CapturePrefix(prefix, FullCaptureBody()))})
	if err != nil {
		t.Fatal(err)
	}
	o := newHandlerLifetime()
	o.capture = opts.capture
	return o.wrap(next, slog.New(slog.NewTextHandler(io.Discard, nil)), config, observers...)
}

func TestWebhookCaptureExactReplayAndEntrySnapshot(t *testing.T) {
	data := append(bytes.Repeat([]byte{0, 255, '\r', '\n', 'a'}, 300), []byte("tail")...)
	recorder := captureTestRecorder(t)
	source := &captureTrackingBody{Reader: bytes.NewReader(data)}
	request := httptest.NewRequest("POST", "/wh/raw?signature=original&a=1&a=2", nil)
	request.Body, request.ContentLength = source, int64(len(data))
	request.Header["X-Signature"] = []string{"first", "second"}
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("getbody")), nil }
	handler := captureTestHandler(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.ContentLength != int64(len(data)) {
			t.Error("capture changed ContentLength")
		}
		clone, err := r.GetBody()
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(clone)
		_ = clone.Close()
		if string(got) != "getbody" {
			t.Error("capture changed GetBody")
		}
		r.Header.Set("X-Signature", "rewritten")
		r.URL.RawQuery = "rewritten=1"
		got, err = io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, data) {
			t.Error("body replay was not exact", err)
		}
		_ = r.Body.Close()
		_ = r.Body.Close()
		_, _ = w.Write([]byte("response"))
	}), DefaultServerConfig(), recorder)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	s := captureTestTake(t, recorder)
	if source.closes.Load() != 1 || source.read.Load() != int64(len(data)) {
		t.Fatal("source ownership/count changed")
	}
	if !bytes.Equal(s.Body.Prefix, data[:1025]) || s.Body.End != httpcapture.OverCaptureLimit || s.Body.ObservedBytes != int64(len(data)) {
		t.Fatal("bad bounded snapshot")
	}
	if n, ok := s.Body.MeasuredBytes.Get(); !ok || n != int64(len(data)) {
		t.Fatal("EOF was not observed")
	}
	if s.Metadata.RawQuery != "signature=original&a=1&a=2" || !s.Metadata.Full {
		t.Fatal("entry query or mode changed")
	}
	for _, field := range s.Metadata.Headers {
		if field.Name == "X-Signature" && strings.Join(field.Values, ",") != "first,second" {
			t.Fatal("header was not snapshotted")
		}
	}
	if string(s.Completion.ResponseHead) != "response" {
		t.Fatal("response head missing with observers disabled")
	}
}

type captureTrackingBody struct {
	*bytes.Reader
	read   atomic.Int64
	closes atomic.Int32
}

func (b *captureTrackingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read.Add(int64(n))
	return n, err
}
func (b *captureTrackingBody) Close() error { b.closes.Add(1); return nil }

func TestWebhookCaptureRoutingAndPrefixSelection(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "capture.post", Method: POST, Access: Public}, StaticPath("/wh/ok"))
	router, err := NewRouter(HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }))
	if err != nil {
		t.Fatal(err)
	}
	recorder := captureTestRecorder(t)
	handler := captureTestHandler(t, router, DefaultServerConfig(), recorder)
	for _, test := range []struct {
		path, method string
		status       int
		captured     bool
	}{
		{"/wh/missing", "POST", 404, true}, {"/wh/ok", "GET", 405, true},
		{"/whx", "POST", 404, false}, {"/wh%2Fx", "POST", 404, true}, {"/wh/ok", "POST", 204, true},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader("body")))
		if response.Code != test.status {
			t.Fatal(test.path, response.Code)
		}
		if !test.captured {
			if recorder.Drain() != nil {
				t.Fatal("segment boundary ignored")
			}
			continue
		}
		s := captureTestTake(t, recorder)
		if s.Metadata.EscapedPath != test.path || string(s.Body.Prefix) != "body" || s.Body.End != httpcapture.EOF || s.Completion.Result.Status != test.status {
			t.Fatal("routing capture mismatch")
		}
		if test.status == 204 && s.Completion.Route != "capture.post" {
			t.Fatal("matched route missing")
		}
		if test.status != 204 && s.Completion.Route != "" {
			t.Fatal("invented matched route")
		}
	}
	base, _ := ParsePathPrefix("/wh")
	debug, _ := ParsePathPrefix("/wh/debug")
	declarations := []CaptureDeclaration{CapturePrefix(base, FullCaptureBody()), CapturePrefix(debug)}
	op := WithWebhookCapture(recorder, declarations...)
	declarations[1] = CaptureDeclaration{}
	options, err := configureServer([]ServerOption{op})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := prepareRequest(httptest.NewRequest("POST", "/wh/debug/x", nil))
	captured := options.capture.begin(r, nil, time.Now())
	if captured == nil || captured.snapshot.Metadata.Full || captured.snapshot.Metadata.Prefix != "/wh/debug" {
		t.Fatal("longest declaration did not win")
	}
	captured.reservation.Complete(captured.snapshot)
}

func TestWebhookCaptureUnreadKernelRejections(t *testing.T) {
	for _, name := range []string{"maintenance", "capacity", "declared-length"} {
		t.Run(name, func(t *testing.T) {
			recorder := captureTestRecorder(t)
			config := DefaultServerConfig()
			config.MaxConcurrentRequests = 1
			entered, release := make(chan struct{}), make(chan struct{})
			handler := captureTestHandler(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { close(entered); <-release; w.WriteHeader(204) }), config, recorder)
			request := httptest.NewRequest("POST", "/wh", nil)
			body := &captureTrackingBody{Reader: bytes.NewReader([]byte("unread"))}
			request.Body, request.ContentLength = body, 6
			want := 503
			switch name {
			case "maintenance":
				gate, err := maintenance.New(maintenance.Policy{})
				if err != nil {
					t.Fatal(err)
				}
				if err := gate.Apply(maintenance.State{Down: true}); err != nil {
					t.Fatal(err)
				}
				request = request.WithContext(maintenance.WithContext(request.Context(), gate))
			case "capacity":
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ordinary", nil))
				}()
				<-entered
				t.Cleanup(func() { close(release); <-finished })
				ctx, cancel := context.WithTimeout(request.Context(), 10*time.Millisecond)
				defer cancel()
				request = request.WithContext(ctx)
			case "declared-length":
				request.ContentLength = config.MaxBodyBytes + 1
				want = 413
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			s := captureTestTake(t, recorder)
			if response.Code != want || s.Completion.Result.Status != want || s.Body.End != httpcapture.NotRead || body.read.Load() != 0 || len(s.Body.Prefix) != 0 {
				t.Fatal("kernel rejection read a body or lost metadata")
			}
		})
	}
}

func TestWebhookCaptureStickyKernelLimitAndRouteLimit(t *testing.T) {
	for _, variant := range []string{"kernel-preread", "kernel-later", "route"} {
		t.Run(variant, func(t *testing.T) {
			routeLimit := variant == "route"
			recorder := captureTestRecorder(t)
			config := DefaultServerConfig()
			config.MaxBodyBytes = 2048
			if variant == "kernel-preread" {
				config.MaxBodyBytes = 512
			}
			var firstErr error
			next := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				data, err := io.ReadAll(r.Body)
				limit := config.MaxBodyBytes
				if routeLimit {
					limit = 7
				}
				large, ok := err.(*stdhttp.MaxBytesError)
				if !ok || large.Limit != limit || int64(len(data)) != limit {
					t.Error("wrong limit offset", len(data), err)
				}
				firstErr = err
				if n, err := r.Body.Read(make([]byte, 1)); n != 0 || err != firstErr {
					t.Error("terminal error was not sticky")
				}
				_ = WriteError(w, r, PayloadTooLarge)
			})
			var handler stdhttp.Handler = next
			if routeLimit {
				route := DefineRoute(RouteSpec{ID: "capture.limit", Method: POST, Access: Public}, StaticPath("/wh/limit"), BodyLimit(7))
				router, err := NewRouter(HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) { next.ServeHTTP(w, r) }))
				if err != nil {
					t.Fatal(err)
				}
				handler = router
			}
			handler = captureTestHandler(t, handler, config, recorder)
			request := httptest.NewRequest("POST", "/wh/limit", strings.NewReader(strings.Repeat("x", 3000)))
			request.ContentLength = -1
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			s := captureTestTake(t, recorder)
			if response.Code != 413 || s.Completion.Result.Outcome != observability.Rejected {
				t.Fatal("limit response missing")
			}
			if routeLimit {
				if s.Body.ReadFailure != "" || s.Body.ObservedBytes != 1025 || s.Completion.Route != "capture.limit" {
					t.Fatal("outer route limit inferred from inner replay")
				}
			} else if s.Body.ReadFailure != httpcapture.BodyLimit || s.Body.ObservedBytes != config.MaxBodyBytes {
				t.Fatal("kernel limit was not observed")
			}
		})
	}
}

func TestWebhookCaptureCompletionBeforeObserversAndPanics(t *testing.T) {
	for _, mode := range []string{"normal", "raw-panic", "abort", "typed-panic"} {
		t.Run(mode, func(t *testing.T) {
			recorder := captureTestRecorder(t)
			var next stdhttp.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				if mode == "raw-panic" {
					panic("private panic")
				}
				if mode == "abort" {
					panic(stdhttp.ErrAbortHandler)
				}
				w.WriteHeader(204)
			})
			if mode == "typed-panic" {
				route := DefineRoute(RouteSpec{ID: "capture.typed", Method: POST, Access: Public}, StaticPath("/wh"))
				endpoint := DefineEndpoint(route, EmptyQuery(), EmptyBody(), EmptyResponse(204))
				router, err := NewRouter(endpoint.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) { panic("private typed panic") }))
				if err != nil {
					t.Fatal(err)
				}
				next = router
			}
			observed := false
			var snapshot httpcapture.Snapshot
			handler := captureTestHandler(t, next, DefaultServerConfig(), recorder, RequestObserverFunc(func(_ context.Context, event RequestEvent) {
				observed = true
				snapshot = captureTestTake(t, recorder)
				if snapshot.Completion.Result != event.Result {
					t.Error("capture and observer results differ")
				}
			}))
			response := httptest.NewRecorder()
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				handler.ServeHTTP(response, httptest.NewRequest("POST", "/wh", nil))
			}()
			if !observed {
				t.Fatal("completion observer did not run")
			}
			aborted := mode == "raw-panic" || mode == "abort"
			if aborted {
				if panicValue != stdhttp.ErrAbortHandler || !snapshot.Completion.Aborted || !snapshot.Completion.Status.IsNull() {
					t.Fatal("raw panic lost abort semantics")
				}
			} else if panicValue != nil || snapshot.Completion.Aborted {
				t.Fatal("ordinary response aborted")
			}
			if mode == "typed-panic" && (response.Code != 500 || snapshot.Completion.Result.Status != 500) {
				t.Fatal("typed panic no longer 500")
			}
		})
	}
}

func TestWebhookCaptureAdmissionRefusalIsTransparent(t *testing.T) {
	recorder := captureTestRecorder(t)
	recorder.Seal()
	var reads int
	next := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		data, _ := io.ReadAll(r.Body)
		reads += len(data)
		_, _ = w.Write(append([]byte("echo:"), data...))
	})
	plain := newHandlerLifetime().wrap(next, slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultServerConfig())
	captured := captureTestHandler(t, next, DefaultServerConfig(), recorder)
	a, b := httptest.NewRecorder(), httptest.NewRecorder()
	plain.ServeHTTP(a, httptest.NewRequest("POST", "/wh", strings.NewReader("a\x00b\r\n")))
	captured.ServeHTTP(b, httptest.NewRequest("POST", "/wh", strings.NewReader("a\x00b\r\n")))
	if a.Code != b.Code || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) || reads != 10 || recorder.Drops()[httpcapture.DropSealed] != 1 {
		t.Fatal("refused capture changed response")
	}
}

func TestWebhookCaptureConcurrent(t *testing.T) {
	recorder := captureTestRecorder(t)
	handler := captureTestHandler(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		data, _ := io.ReadAll(r.Body)
		_, _ = w.Write(data)
	}), DefaultServerConfig(), recorder)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("POST", "/wh", strings.NewReader("exact")))
			if response.Body.String() != "exact" {
				t.Error("concurrent replay changed")
			}
		})
	}
	wg.Wait()
	ids := make(map[string]bool)
	for range 100 {
		s := captureTestTake(t, recorder)
		id := string(s.Metadata.RequestID)
		if ids[id] || id == "" || string(s.Body.Prefix) != "exact" || string(s.Completion.ResponseHead) != "exact" {
			t.Fatal("concurrent snapshot corrupted")
		}
		ids[id] = true
	}
}

func TestWebhookCaptureAdmissionProxyAndBounds(t *testing.T) {
	recorder := captureTestRecorder(t)
	prefix, _ := ParsePathPrefix("/wh")
	proxy := TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Headers: []ProxyHeader{XForwardedForHeader()}})
	options, err := configureServer([]ServerOption{WithWebhookCapture(recorder, CapturePrefix(prefix)), WithAdmissionProxy(proxy)})
	if err != nil {
		t.Fatal(err)
	}
	o := newHandlerLifetime()
	o.capture, o.proxy = options.capture, options.proxy
	handler := o.wrap(stdhttp.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultServerConfig())
	r := httptest.NewRequest("POST", "/wh/"+strings.Repeat("p", 5000)+"?"+strings.Repeat("q", 20000), nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.4")
	r.Header.Set("X-Large", strings.Repeat("h", 10000))
	handler.ServeHTTP(httptest.NewRecorder(), r)
	s := captureTestTake(t, recorder)
	if s.Metadata.ClientIP.String() != "203.0.113.4" || len(s.Metadata.EscapedPath) != 4096 || !s.Metadata.PathTruncated || len(s.Metadata.RawQuery) != 16384 || !s.Metadata.QueryTruncated || !s.Metadata.HeadersTruncated {
		t.Fatal("unbounded metadata or wrong proxy IP")
	}
}

func TestWebhookCaptureHeaderBudgetIsDeterministic(t *testing.T) {
	header := stdhttp.Header{}
	for i := range 200 {
		header.Set(fmt.Sprintf("X-Field-%03d", i), strings.Repeat("v", 200))
	}
	want, truncated := captureHeaders(header)
	if !truncated || len(want) == 0 || want[0].Name != "X-Field-000" {
		t.Fatal("header snapshot did not keep the lowest canonical names first", len(want), truncated)
	}
	for i := 1; i < len(want); i++ {
		if want[i-1].Name >= want[i].Name {
			t.Fatal("header snapshot is not sorted by canonical name")
		}
	}
	for range 20 {
		got, _ := captureHeaders(header)
		if len(got) != len(want) || got[len(got)-1].Name != want[len(want)-1].Name {
			t.Fatal("header budget depends on map iteration order")
		}
	}
}

type captureFixedClock struct{}

func (captureFixedClock) Now() time.Time { return time.Unix(1700000000, 0) }

func TestWebhookCaptureVerifierReceivesSignedBytes(t *testing.T) {
	cfg := webhook.DefaultConfig("billing", webhook.Standard)
	verifier, err := webhook.New(cfg, int64(7), []secret.String{secret.New("whsec_MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=")}, captureFixedClock{})
	if err != nil {
		t.Fatal(err)
	}
	next, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "{}" {
			t.Error("signed bytes changed")
		}
		w.WriteHeader(204)
	}), VerifyWebhook(verifier))
	if err != nil {
		t.Fatal(err)
	}
	recorder := captureTestRecorder(t)
	handler := captureTestHandler(t, next, DefaultServerConfig(), recorder)
	r := httptest.NewRequest("POST", "/wh", strings.NewReader("{}"))
	r.Header.Set("Webhook-Id", "delivery_1")
	r.Header.Set("Webhook-Timestamp", "1700000000")
	r.Header.Set("Webhook-Signature", "v1,hkMcggvUp1HOnbVeYmO9M2Z4ZI0JbDLURtZ6I4qC5as=")
	r.Header.Set("Idempotency-Key", "unsigned")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	s := captureTestTake(t, recorder)
	if w.Code != 204 || string(s.Body.Prefix) != "{}" || s.Body.End != httpcapture.EOF {
		t.Fatal("verifier did not accept exact replay")
	}
	for _, header := range s.Metadata.Headers {
		if header.Name == "Idempotency-Key" && header.Values[0] != "unsigned" {
			t.Fatal("middleware rewrote captured header")
		}
	}
}
