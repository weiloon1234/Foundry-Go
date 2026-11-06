package http

import (
	"bufio"
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func spaRuntimeLifecycleProxy(t *testing.T, d SPARuntime, transport stdhttp.RoundTripper, pool httputil.BufferPool) stdhttp.Handler {
	t.Helper()
	handler, err := d.WrapProxy(&httputil.ReverseProxy{
		Rewrite:    func(r *httputil.ProxyRequest) { r.SetURL(&url.URL{Scheme: "http", Host: "example.test"}) },
		Transport:  transport,
		BufferPool: pool,
	}, func(*stdhttp.Request) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func spaRuntimeLifecycleResponse(body io.ReadCloser) *stdhttp.Response {
	return &stdhttp.Response{StatusCode: 200, Header: stdhttp.Header{"Content-Type": []string{"text/html"}}, Body: body}
}

func spaRuntimeAssertRecovery(t *testing.T, proxy stdhttp.Handler, built *Router) {
	t.Helper()
	for _, target := range []struct {
		handler stdhttp.Handler
		path    string
	}{{proxy, "/healthy"}, {built, "/app/"}} {
		w := spaRuntimeRequest(target.handler, "GET", target.path)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `id="foundry-portal-runtime"`) {
			t.Fatalf("healthy %s: status %d, body %s", target.path, w.Code, w.Body.String())
		}
	}
}

// Run in an owned goroutine so the test can observe Goexit as well as panic.
func spaRuntimeObserveExit(t *testing.T, run func()) (recovered any, returned bool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recovered = recover() }()
		run()
		returned = true
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy callback did not finish")
	}
	return recovered, returned
}

type spaRuntimeExitWriter struct {
	*httptest.ResponseRecorder
	header bool
	fail   func()
}

func (w *spaRuntimeExitWriter) WriteHeader(code int) {
	if w.header {
		w.fail()
	}
	w.ResponseRecorder.WriteHeader(code)
}
func (w *spaRuntimeExitWriter) Write(data []byte) (int, error) {
	if !w.header {
		w.fail()
	}
	return w.ResponseRecorder.Write(data)
}

type spaRuntimeExitPool struct {
	put    bool
	failed atomic.Bool
	fail   func()
}

func (p *spaRuntimeExitPool) Get() []byte {
	if !p.put && !p.failed.Swap(true) {
		p.fail()
	}
	return make([]byte, 32<<10)
}
func (p *spaRuntimeExitPool) Put([]byte) {
	if p.put && !p.failed.Swap(true) {
		p.fail()
	}
}

func TestSPARuntimeProxyDownstreamExitReleasesSharedCapacity(t *testing.T) {
	for _, where := range []string{"WriteHeader", "Write", "BufferPool.Get", "BufferPool.Put"} {
		for _, exit := range []string{"panic", "Goexit"} {
			t.Run(where+"/"+exit, func(t *testing.T) {
				config := DefaultSPARuntimeConfig()
				config.MaxConcurrent = 1
				d := spaRuntimeTestDescriptor("ok", config)
				built := spaRuntimeTestRouter(t, d, "<head></head>")
				fail := func() {
					if exit == "Goexit" {
						runtime.Goexit()
					}
					panic("downstream sentinel")
				}
				var pool httputil.BufferPool
				var writer stdhttp.ResponseWriter = httptest.NewRecorder()
				if strings.HasPrefix(where, "BufferPool") {
					pool = &spaRuntimeExitPool{put: where == "BufferPool.Put", fail: fail}
				} else {
					writer = &spaRuntimeExitWriter{ResponseRecorder: httptest.NewRecorder(), header: where == "WriteHeader", fail: fail}
				}
				proxy := spaRuntimeLifecycleProxy(t, d, spaRuntimeTestTransport(func(*stdhttp.Request) (*stdhttp.Response, error) {
					return spaRuntimeLifecycleResponse(io.NopCloser(strings.NewReader("<head></head>"))), nil
				}), pool)
				recovered, returned := spaRuntimeObserveExit(t, func() { proxy.ServeHTTP(writer, httptest.NewRequest("GET", "/fail", nil)) })
				if returned || exit == "panic" && recovered != "downstream sentinel" || exit == "Goexit" && recovered != nil {
					t.Fatalf("exit changed: returned=%v panic=%v", returned, recovered)
				}
				spaRuntimeAssertRecovery(t, proxy, built)
			})
		}
	}
}

type spaRuntimeCleanupBody struct {
	io.Reader
	close func() error
}

func (b *spaRuntimeCleanupBody) Close() error { return b.close() }

type spaRuntimeFailingReader func([]byte) (int, error)

func (f spaRuntimeFailingReader) Read(p []byte) (int, error) { return f(p) }

func TestSPARuntimeProxyFailureRetainsAdmissionThroughClose(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	config := DefaultSPARuntimeConfig()
	config.MaxConcurrent = 1
	d := DefineSPARuntime(spaRuntimeTestCodec(), func(_ context.Context, r *stdhttp.Request) (SPARuntimeTestDTO, error) {
		if r.URL.Path == "/fail" {
			return SPARuntimeTestDTO{}, errors.New("private loader failure")
		}
		return SPARuntimeTestDTO{Text: "ok"}, nil
	}, config)
	var closes atomic.Int32
	proxy := spaRuntimeLifecycleProxy(t, d, spaRuntimeTestTransport(func(r *stdhttp.Request) (*stdhttp.Response, error) {
		body := io.NopCloser(strings.NewReader("<head></head>"))
		if r.URL.Path == "/fail" {
			body = &spaRuntimeCleanupBody{Reader: strings.NewReader("<head></head>"), close: func() error {
				closes.Add(1)
				close(entered)
				<-release
				return nil
			}}
		}
		return spaRuntimeLifecycleResponse(body), nil
	}), nil)
	built := spaRuntimeTestRouter(t, d, "<head></head>")
	done := make(chan struct{})
	failed := httptest.NewRecorder()
	go func() { defer close(done); proxy.ServeHTTP(failed, httptest.NewRequest("GET", "/fail", nil)) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("source close not entered")
	}
	for _, target := range []struct {
		handler stdhttp.Handler
		path    string
	}{{proxy, "/healthy"}, {built, "/app/"}} {
		if got := spaRuntimeRequest(target.handler, "GET", target.path); got.Code != 503 {
			t.Errorf("capacity released during Close: %s status %d", target.path, got.Code)
		}
	}
	select {
	case <-done:
		t.Error("request returned before Close exited")
	default:
	}
	unblock.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish after Close")
	}
	if failed.Code != 503 || closes.Load() != 1 {
		t.Fatal(failed.Code, closes.Load())
	}
	spaRuntimeAssertRecovery(t, proxy, built)
}

func TestSPARuntimeProxyReadLoaderAndCloseFailuresRecover(t *testing.T) {
	for _, phase := range []string{"read", "loader"} {
		for _, operationExit := range []string{"error", "panic", "Goexit"} {
			for _, closeExit := range []string{"error", "panic", "Goexit"} {
				t.Run(phase+"/"+operationExit+"/close-"+closeExit, func(t *testing.T) {
					fail := func(exit string) error {
						switch exit {
						case "panic":
							panic("private source panic")
						case "Goexit":
							runtime.Goexit()
						}
						return errors.New("private source error")
					}
					config := DefaultSPARuntimeConfig()
					config.MaxConcurrent = 1
					d := DefineSPARuntime(spaRuntimeTestCodec(), func(_ context.Context, r *stdhttp.Request) (SPARuntimeTestDTO, error) {
						if phase == "loader" && r.URL.Path == "/fail" {
							return SPARuntimeTestDTO{}, fail(operationExit)
						}
						return SPARuntimeTestDTO{Text: "ok"}, nil
					}, config)
					var closes atomic.Int32
					proxy := spaRuntimeLifecycleProxy(t, d, spaRuntimeTestTransport(func(r *stdhttp.Request) (*stdhttp.Response, error) {
						if r.URL.Path != "/fail" {
							return spaRuntimeLifecycleResponse(io.NopCloser(strings.NewReader("<head></head>"))), nil
						}
						var reader io.Reader = strings.NewReader("<head></head>")
						if phase == "read" {
							reader = spaRuntimeFailingReader(func([]byte) (int, error) { return 0, fail(operationExit) })
						}
						return spaRuntimeLifecycleResponse(&spaRuntimeCleanupBody{Reader: reader, close: func() error { closes.Add(1); return fail(closeExit) }}), nil
					}), nil)
					built := spaRuntimeTestRouter(t, d, "<head></head>")
					w := httptest.NewRecorder()
					recovered, returned := spaRuntimeObserveExit(t, func() { proxy.ServeHTTP(w, httptest.NewRequest("GET", "/fail", nil)) })
					if !returned || recovered != nil || w.Code != 503 || closes.Load() != 1 || strings.Contains(w.Body.String(), "private") {
						t.Fatal(returned, recovered, w.Code, closes.Load(), w.Body.String())
					}
					spaRuntimeAssertRecovery(t, proxy, built)
				})
			}
		}
	}
}

func TestSPARuntimeProxyJoinsSourceAndCloseErrors(t *testing.T) {
	loadErr, closeErr := errors.New("loader sentinel"), errors.New("close sentinel")
	config := DefaultSPARuntimeConfig()
	config.MaxConcurrent = 1
	d := DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
		return SPARuntimeTestDTO{}, loadErr
	}, config)
	var closes int
	response := spaRuntimeLifecycleResponse(&spaRuntimeCleanupBody{Reader: strings.NewReader("<head></head>"), close: func() error { closes++; return closeErr }})
	err := d.state.proxyDocument(response, httptest.NewRequest("GET", "/", nil))
	if !errors.Is(err, loadErr) || !errors.Is(err, closeErr) || closes != 1 || response.Body != stdhttp.NoBody {
		t.Fatal("lost failure identity or source ownership", err, closes)
	}
}

func TestSPARuntimeProxyRejectsUndeclaredChunkedTrailers(t *testing.T) {
	config := DefaultSPARuntimeConfig()
	config.MaxConcurrent = 1
	d := spaRuntimeTestDescriptor("ok", config)
	var initialEmpty, lateTrailers bool
	var closes int
	proxy := spaRuntimeLifecycleProxy(t, d, spaRuntimeTestTransport(func(r *stdhttp.Request) (*stdhttp.Response, error) {
		if r.URL.Path != "/fail" {
			return spaRuntimeLifecycleResponse(io.NopCloser(strings.NewReader("<head></head>"))), nil
		}
		// No Trailer header: net/http discovers these trailers only at chunked EOF.
		raw := "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nTransfer-Encoding: chunked\r\n\r\nd\r\n<head></head>\r\n0\r\nDigest: stale-digest\r\nETag: stale-etag\r\n\r\n"
		response, err := stdhttp.ReadResponse(bufio.NewReader(strings.NewReader(raw)), r)
		if err != nil {
			return nil, err
		}
		initialEmpty = len(response.Trailer) == 0 && !hasResponseTrailers(response.Header)
		body := response.Body
		response.Body = &spaRuntimeCleanupBody{Reader: body, close: func() error {
			closes++
			lateTrailers = response.Trailer.Get("Digest") == "stale-digest" && response.Trailer.Get("ETag") == "stale-etag"
			return body.Close()
		}}
		return response, nil
	}), nil)
	w := spaRuntimeRequest(proxy, "GET", "/fail")
	if w.Code != 503 || !initialEmpty || !lateTrailers || closes != 1 {
		t.Fatal(w.Code, initialEmpty, lateTrailers, closes)
	}
	for _, name := range []string{"Trailer", "Digest", "ETag", stdhttp.TrailerPrefix + "Digest", stdhttp.TrailerPrefix + "ETag"} {
		if len(w.Header().Values(name)) != 0 {
			t.Fatal("stale trailer emitted", name)
		}
	}
	if strings.Contains(w.Body.String(), "<head>") {
		t.Fatal("partial transformed document emitted")
	}
	spaRuntimeAssertRecovery(t, proxy, spaRuntimeTestRouter(t, d, "<head></head>"))
}

func TestSPARuntimeProxyRejectsTrailersAddedOnClose(t *testing.T) {
	for _, header := range []bool{false, true} {
		t.Run(map[bool]string{false: "Trailer map", true: "Trailer header"}[header], func(t *testing.T) {
			config := DefaultSPARuntimeConfig()
			config.MaxConcurrent = 1
			d := spaRuntimeTestDescriptor("ok", config)
			var closes int
			proxy := spaRuntimeLifecycleProxy(t, d, spaRuntimeTestTransport(func(r *stdhttp.Request) (*stdhttp.Response, error) {
				response := spaRuntimeLifecycleResponse(io.NopCloser(strings.NewReader("<head></head>")))
				if r.URL.Path == "/fail" {
					response.Body = &spaRuntimeCleanupBody{Reader: strings.NewReader("<head></head>"), close: func() error {
						closes++
						if header {
							response.Header.Set("Trailer", "Digest")
						} else {
							response.Trailer = stdhttp.Header{"Digest": []string{"stale"}}
						}
						return nil
					}}
				}
				return response, nil
			}), nil)
			w := spaRuntimeRequest(proxy, "GET", "/fail")
			if w.Code != 503 || closes != 1 || hasResponseTrailers(w.Header()) || w.Header().Get("Digest") != "" {
				t.Fatal(w.Code, closes, w.Header())
			}
			spaRuntimeAssertRecovery(t, proxy, spaRuntimeTestRouter(t, d, "<head></head>"))
		})
	}
}
