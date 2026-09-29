package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/weiloon1234/Foundry-Go/validation"
)

type slowBody struct {
	delay time.Duration
	data  *strings.Reader
}

func (b *slowBody) Read(p []byte) (int, error) {
	if b.delay > 0 {
		time.Sleep(b.delay)
		b.delay = 0
	}
	return b.data.Read(p)
}
func (*slowBody) Close() error { return nil }

// A deadline while the request is still being read or decoded means the client
// was too slow (408). Deadlines after decoding are the server's budget (503).
func TestRequestDeadlineClassificationFollowsLifecyclePhase(t *testing.T) {
	t.Parallel()
	router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		t.Error("slow body reached the handler")
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, nil)
	request.Body, request.ContentLength = &slowBody{delay: 60 * time.Millisecond, data: strings.NewReader(`{"name":"ok"}`)}, -1
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	deadlineBoundary(newHandlerLifetime(), router, 20*time.Millisecond).ServeHTTP(response, request)
	if response.Code != 408 || decodeFailure(t, response).Code != RequestTimeout {
		t.Fatalf("slow request body: %d %s", response.Code, response.Body.String())
	}

	locked := DefineError("item_locked", 409, "The item is locked.")
	for _, test := range []struct {
		name   string
		result func(context.Context) error
		status int
	}{
		{"declared-error-wins", func(context.Context) error { return locked }, 409},
		{"unclassified-deadline", func(ctx context.Context) error { return ctx.Err() }, 503},
	} {
		router, err := NewRouter(patchEndpoint().WithErrors(locked).Handle(func(ctx context.Context, _ endpointRequest) (EndpointReply, error) {
			<-ctx.Done()
			return EndpointReply{}, test.result(ctx)
		}))
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"ok"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		deadlineBoundary(newHandlerLifetime(), router, 10*time.Millisecond).ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s: %d %s", test.name, response.Code, response.Body.String())
		}
	}
}

type countingLookup struct{ calls atomic.Int32 }

func (*countingLookup) Validate() error { return nil }
func (l *countingLookup) Exists(context.Context, string) (bool, error) {
	l.calls.Add(1)
	return false, nil
}

// Authorization precedes validation (FormRequest order): a denied caller never
// reaches database-backed rules such as Unique.
func TestRequestAuthorizationRunsBeforeValidationRules(t *testing.T) {
	t.Parallel()
	name := validation.DefineField("name", func(b EndpointPatch) string { return b.Name })
	for _, allow := range []bool{false, true} {
		lookup := &countingLookup{}
		var authorized atomic.Int32
		endpoint := patchEndpoint().WithBodyValidation(name.Rules(validation.Unique[string](lookup))).WithAuthorization(func(context.Context, endpointRequest) error {
			authorized.Add(1)
			if !allow {
				return Forbidden
			}
			return nil
		})
		router, err := NewRouter(endpoint.Handle(func(context.Context, endpointRequest) (EndpointReply, error) { return EndpointReply{}, nil }))
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"Jane"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		switch {
		case !allow && (response.Code != 403 || lookup.calls.Load() != 0):
			t.Fatalf("denied caller reached validation: %d lookups=%d", response.Code, lookup.calls.Load())
		case allow && (response.Code != 201 || lookup.calls.Load() != 1):
			t.Fatalf("authorized caller skipped validation: %d lookups=%d", response.Code, lookup.calls.Load())
		case authorized.Load() != 1:
			t.Fatal("authorization did not run exactly once")
		}
	}
}

func timeoutRoute(id RouteID, timeout time.Duration, handler func(stdhttp.ResponseWriter, *stdhttp.Request)) RouteRegistration {
	route := DefineRoute(RouteSpec{ID: id, Method: GET, Access: Public}, StaticPath("/"+string(id)))
	if timeout > 0 {
		route = route.WithTimeout(timeout)
	}
	return route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) { handler(w, r) })
}

// A route's declared timeout replaces the kernel RequestTimeout after routing,
// in both directions, while client cancellation still propagates.
func TestRouteTimeoutReplacesKernelRequestTimeout(t *testing.T) {
	t.Parallel()
	started := time.Now()
	router, err := NewRouter(
		timeoutRoute("long", 400*time.Millisecond, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			time.Sleep(60 * time.Millisecond)
			deadline, ok := r.Context().Deadline()
			if r.Context().Err() != nil || !ok || deadline.Before(started.Add(300*time.Millisecond)) {
				t.Error("long route did not replace the kernel deadline", r.Context().Err())
			}
			if r.Context().Value(applicationValue{}) != "kept" || RequestID(r.Context()) == "" {
				t.Error("replaced deadline lost request values")
			}
			w.WriteHeader(204)
		}),
		timeoutRoute("short", 10*time.Millisecond, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
				t.Error("short route kept the longer kernel deadline")
			}
			w.WriteHeader(204)
		}),
		timeoutRoute("held", time.Minute, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
				t.Error("client cancellation did not reach an extended route")
			}
			w.WriteHeader(204)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.RequestTimeout = 20 * time.Millisecond
	kernel := newHandlerLifetime().wrap(router, slog.New(slog.NewTextHandler(io.Discard, nil)), config)
	parent := context.WithValue(t.Context(), applicationValue{}, "kept")
	for _, path := range []string{"/long", "/short"} {
		response := httptest.NewRecorder()
		kernel.ServeHTTP(response, httptest.NewRequestWithContext(parent, "GET", path, nil))
		if response.Code != 204 {
			t.Fatal(path, response.Code)
		}
	}
	client, cancel := context.WithCancel(parent)
	time.AfterFunc(40*time.Millisecond, cancel)
	kernel.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(client, "GET", "/held", nil))

	standalone := httptest.NewRecorder()
	router.ServeHTTP(standalone, httptest.NewRequest("GET", "/short", nil))
	if standalone.Code != 204 {
		t.Fatal("standalone route timeout", standalone.Code)
	}
	for _, invalid := range []time.Duration{0, -time.Second, MaxRouteTimeout + 1} {
		if DefineRoute(RouteSpec{ID: "bad", Method: GET, Access: Public}, StaticPath("/bad")).WithTimeout(invalid).Validate() == nil {
			t.Fatal("invalid route timeout accepted", invalid)
		}
	}
	if info := (func() RouteInfo {
		r, _ := NewRouter(timeoutRoute("x", time.Minute, func(stdhttp.ResponseWriter, *stdhttp.Request) {}))
		return r.Routes()[0]
	})(); info.Timeout != time.Minute {
		t.Fatal("route timeout was not inspectable")
	}
}

// A route can accept a larger body than the server-wide MaxBodyBytes without
// raising that default for other routes, including through ApplyMiddleware.
func TestRouteBodyLimitReplacesKernelDefault(t *testing.T) {
	t.Parallel()
	read := func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			_ = WriteError(w, r, PayloadTooLarge.WithCause(err))
			return
		}
		w.Header().Set("X-Read", strconv.Itoa(len(data)))
		w.WriteHeader(204)
	}
	upload := DefineRoute(RouteSpec{ID: "upload", Method: POST, Access: Public}, StaticPath("/upload")).WithBodyLimit(64).HandleRaw(read)
	ordinary := DefineRoute(RouteSpec{ID: "ordinary", Method: POST, Access: Public}, StaticPath("/ordinary")).HandleRaw(read)
	router, err := NewRouter(upload, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := ApplyMiddleware(router)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.MaxBodyBytes = 8
	kernel := newHandlerLifetime().wrap(wrapped, slog.New(slog.NewTextHandler(io.Discard, nil)), config)
	body := strings.Repeat("x", 32)
	for _, test := range []struct {
		path    string
		chunked bool
		status  int
	}{
		{"/upload", false, 204}, {"/upload", true, 204},
		{"/ordinary", false, 413}, {"/ordinary", true, 413},
	} {
		request := httptest.NewRequest("POST", test.path, strings.NewReader(body))
		if test.chunked {
			request.ContentLength = -1
		}
		response := httptest.NewRecorder()
		kernel.ServeHTTP(response, request)
		if response.Code != test.status || test.status == 204 && response.Header().Get("X-Read") != "32" {
			t.Fatalf("%s chunked=%v: %d %s", test.path, test.chunked, response.Code, response.Body.String())
		}
	}
	large := httptest.NewRequest("POST", "/upload", strings.NewReader(strings.Repeat("x", 65)))
	response := httptest.NewRecorder()
	kernel.ServeHTTP(response, large)
	if response.Code != 413 {
		t.Fatal("route ceiling was not enforced", response.Code)
	}
	if DefineRoute(RouteSpec{ID: "bad", Method: POST, Access: Public}, StaticPath("/bad")).WithBodyLimit(0).Validate() == nil {
		t.Fatal("invalid route body limit accepted")
	}
}

// Every typed-endpoint 5xx reaches WriteError's redacted server-failure report,
// and an oversized response names the exceeded EndpointLimits.Response bound.
func TestTypedEndpointServerFailuresAreReported(t *testing.T) {
	t.Parallel()
	limits := DefaultEndpointLimits()
	limits.Response.Nodes = 4
	failing := validation.Custom(validation.Spec{ID: "app.infrastructure", Message: "Unavailable."}, func(context.Context, EndpointPatch) (bool, error) {
		return false, errors.New("password=hunter2")
	})
	for _, test := range []struct {
		name     string
		endpoint Endpoint[userPath, endpointParameters, EndpointPatch, EndpointReply]
		note     string
	}{
		{"response-limit", patchEndpoint().WithLimits(limits), "node bound"},
		{"validation-infrastructure", patchEndpoint().WithBodyValidation(failing), "HTTP request failed"},
	} {
		var output bytes.Buffer
		router, err := NewRouter(test.endpoint.Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
			return EndpointReply{ID: "a", Name: "b"}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		kernel := newHandlerLifetime().wrap(router, slog.New(slog.NewJSONHandler(&output, nil)), DefaultServerConfig())
		request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"Jane"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		kernel.ServeHTTP(response, request)
		logged := output.String()
		if response.Code != 500 || !strings.Contains(logged, "HTTP request failed") || !strings.Contains(logged, "items.update") || !strings.Contains(logged, test.note) {
			t.Fatalf("%s: %d %s", test.name, response.Code, logged)
		}
		if strings.Contains(logged, "hunter2") {
			t.Fatalf("%s: report formatted an application error", test.name)
		}
	}
}

// Ordinary large list responses fit the default response limits.
func TestDefaultResponseLimitsServeLargeLists(t *testing.T) {
	t.Parallel()
	items := make([]BenchmarkListItem, 30000)
	for i := range items {
		items[i] = BenchmarkListItem{ID: "item-" + strconv.Itoa(i), Name: "Example item " + strconv.Itoa(i), Count: int64(i), Active: i%2 == 0}
	}
	route := DefineRoute(RouteSpec{ID: "list", Method: GET, Access: Public}, StaticPath("/list"))
	router, err := NewRouter(DefineEndpoint(route, EmptyQuery(), EmptyBody(), JSONResponse(200, benchmarkReplyJSON())).Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (BenchmarkListReply, error) {
		return BenchmarkListReply{Items: items, Total: int64(len(items))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/list", nil))
	if response.Code != 200 || response.Body.Len() <= 2<<20 {
		t.Fatalf("large list: %d, %d bytes", response.Code, response.Body.Len())
	}
	var reply BenchmarkListReply
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || len(reply.Items) != len(items) {
		t.Fatal("large list response changed", err)
	}
}

// StaticPath("") within a scope matches the scope prefix itself.
func TestScopeRootRouteHasNoForcedTrailingSlash(t *testing.T) {
	t.Parallel()
	scope := DefineScope("/api", "api")
	root := DefineRoute(RouteSpec{ID: "index", Method: GET, Access: Public}, StaticPath("")).Within(scope)
	slash := DefineRoute(RouteSpec{ID: "slash", Method: GET, Access: Public}, StaticPath("/")).Within(scope)
	if root.Pattern() != "/api" || slash.Pattern() != "/api/" || root.ID() != "api.index" {
		t.Fatal("scope root patterns", root.Pattern(), slash.Pattern(), root.ID())
	}
	if location, err := root.URL(NoPath{}); err != nil || location != "/api" {
		t.Fatal("scope root URL", location, err)
	}
	router, err := NewRouter(root.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }))
	if err != nil {
		t.Fatal(err)
	}
	for path, status := range map[string]int{"/api": 204, "/api/": 404} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != status {
			t.Fatal(path, response.Code)
		}
	}
	if DefineRoute(RouteSpec{ID: "empty", Method: GET, Access: Public}, StaticPath("")).Validate() == nil {
		t.Fatal("unscoped empty pattern accepted")
	}
	if DefineRoute(RouteSpec{ID: "empty", Method: GET, Access: Public}, StaticPath("")).Within(DefineScope("", "api")).Validate() == nil {
		t.Fatal("empty pattern accepted within an empty path scope")
	}
}

// The router matches once: registered routes run once and native canonical
// redirects still pass through unchanged.
func TestRouterMatchesOnceAndKeepsNativeRedirects(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	router, err := NewRouter(DefineRoute(RouteSpec{ID: "nested", Method: GET, Access: Public}, StaticPath("/a/b")).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) {
		calls.Add(1)
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/a/b", nil))
	if response.Code != 204 || calls.Load() != 1 {
		t.Fatal("matched route", response.Code, calls.Load())
	}
	redirect := httptest.NewRecorder()
	redirect.Header().Set("X-Policy", "kept")
	router.ServeHTTP(redirect, httptest.NewRequest("GET", "/a//b?x=1", nil))
	// The native mux owns the canonical redirect status (307 in current Go).
	if redirect.Code/100 != 3 || redirect.Header().Get("Location") != "/a/b?x=1" || redirect.Header().Get("X-Policy") != "kept" || calls.Load() != 1 {
		t.Fatal("native redirect", redirect.Code, redirect.Header())
	}
}

type readerFromRecorder struct {
	*httptest.ResponseRecorder
	sources []io.Reader
}

func (w *readerFromRecorder) ReadFrom(source io.Reader) (int64, error) {
	w.sources = append(w.sources, source)
	return io.Copy(struct{ io.Writer }{w.ResponseRecorder}, source)
}

// A local file reaches the native writer's ReaderFrom (sendfile on a TCP
// connection) through the kernel's observed writer, with length verification.
func TestLocalDownloadReachesNativeReaderFrom(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	payload := strings.Repeat("0123456789", 5000)
	if err := os.WriteFile(filepath.Join(directory, "report.bin"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	route := DefineRoute(RouteSpec{ID: "report", Method: GET, Access: Public}, StaticPath("/report"))
	router, err := NewRouter(DefineEndpoint(route, EmptyQuery(), EmptyBody(), DownloadResponse("application/octet-stream")).Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (Download, error) {
		return LocalDownload(root, "report.bin"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.AccessLog = true
	kernel := newHandlerLifetime().wrap(router, slog.New(slog.NewTextHandler(io.Discard, nil)), config)
	for _, rangeHeader := range []string{"", "bytes=10-19"} {
		writer := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
		request := httptest.NewRequest("GET", "/report", nil)
		if rangeHeader != "" {
			request.Header.Set("Range", rangeHeader)
		}
		kernel.ServeHTTP(writer, request)
		want := payload
		if rangeHeader != "" {
			want = payload[10:20]
		}
		if writer.Body.String() != want || writer.Header().Get("Content-Length") != strconv.Itoa(len(want)) {
			t.Fatalf("range %q: %d %d bytes", rangeHeader, writer.Code, writer.Body.Len())
		}
		if len(writer.sources) != 1 {
			t.Fatalf("range %q: native ReaderFrom calls %d", rangeHeader, len(writer.sources))
		}
		limited, ok := writer.sources[0].(*io.LimitedReader)
		if !ok {
			t.Fatalf("native ReaderFrom source %T", writer.sources[0])
		}
		if _, ok := limited.R.(*os.File); !ok {
			t.Fatalf("native ReaderFrom did not receive the local file: %T", limited.R)
		}
	}
}

// Files without a modification time (embed.FS) get a content validator, so
// revalidation can answer 304.
func TestTimelessAssetsUseContentEntityTags(t *testing.T) {
	t.Parallel()
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{"app.js": {Data: []byte("console.log(1)")}})))
	router, err := NewRouter(assets.Mount("assets", "/assets").Register())
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest("GET", "/assets/app.js", nil))
	tag := first.Header().Get("ETag")
	if first.Code != 200 || !strings.HasPrefix(tag, `"`) || len(tag) != 66 {
		t.Fatal("timeless asset validator", first.Code, tag)
	}
	again := httptest.NewRequest("GET", "/assets/app.js", nil)
	again.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, again)
	if second.Code != 304 {
		t.Fatal("timeless asset revalidation", second.Code)
	}
}
