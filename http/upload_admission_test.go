package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func admittedEndpoint(t *testing.T, pool *UploadPool) Endpoint[NoPath, NoQuery, multipartRequest, NoContent] {
	t.Helper()
	e := multipartEndpoint("")
	e.body = MultipartBody(MultipartAdmission(multipartForm(""), pool))
	e.route = ConfigureRoute(e.route, BodyLimit(e.limits.Multipart.Bytes), Timeout(time.Minute))
	return e
}

func uploadTestPool(t *testing.T, slots int, bytes int64) *UploadPool {
	t.Helper()
	p, err := NewUploadPool(UploadPoolConfig{Directory: t.TempDir(), MaxActive: slots, MaxBytes: bytes})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

// Direct-handler tests provide the serving owner that Server.Run normally owns.
func claimedUploadTestPool(t *testing.T, slots int, bytes int64) *UploadPool {
	t.Helper()
	pool := uploadTestPool(t, slots, bytes)
	if err := pool.pool.Claim(); err != nil {
		t.Fatal(err)
	}
	return pool
}

type uploadUnreadBody struct{ reads atomic.Int32 }

func (b *uploadUnreadBody) Read([]byte) (int, error) { b.reads.Add(1); return 0, io.EOF }
func (*uploadUnreadBody) Close() error               { return nil }

func TestUploadAdmissionBeforeMiddlewareAndSmallRequestIsolation(t *testing.T) {
	budget := DefaultEndpointLimits().Multipart.Bytes
	for _, chunked := range []bool{false, true} {
		t.Run(map[bool]string{false: "known", true: "chunked"}[chunked], func(t *testing.T) {
			pool := claimedUploadTestPool(t, 1, budget)
			entered, release := make(chan struct{}), make(chan struct{})
			e := admittedEndpoint(t, pool)
			var middlewareCalls atomic.Int32
			router, err := NewRouter(e.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				close(entered)
				<-release
				return NoContent{}, nil
			}), DefineEndpoint(DefineRoute(RouteSpec{ID: "small", Method: POST, Access: Public}, StaticPath("/small")), EmptyQuery(), MultipartBody(multipartForm(t.TempDir())), EmptyResponse(204)).Handle(func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }),
				DefineEndpoint(DefineRoute(RouteSpec{ID: "json", Method: GET, Access: Public}, StaticPath("/small-json")), EmptyQuery(), EmptyBody(), JSONResponse(200, endpointPatchJSON())).Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (EndpointPatch, error) {
					return EndpointPatch{}, nil
				}))
			if err != nil {
				t.Fatal(err)
			}
			handler, err := ApplyMiddleware(router, DefineMiddleware("count", func(next stdhttp.Handler) (stdhttp.Handler, error) {
				return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { middlewareCalls.Add(1); next.ServeHTTP(w, r) }), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			config := DefaultServerConfig()
			config.MaxConcurrentRequests = 1
			kernel := newHandlerLifetime().wrap(handler, slog.New(slog.NewTextHandler(io.Discard, nil)), config)
			wire, media := multipartWire(t, primaryUpload("hello"))
			request := func(path string) *stdhttp.Request {
				r := httptest.NewRequest("POST", path, bytes.NewReader(wire))
				r.Header.Set("Content-Type", media)
				if chunked {
					r.ContentLength = -1
				}
				return r
			}
			done := make(chan struct{})
			go func() { defer close(done); kernel.ServeHTTP(httptest.NewRecorder(), request("/uploads")) }()
			<-entered
			if active, reserved := pool.Usage(); active != 1 || reserved != budget {
				t.Fatalf("reservation = %d/%d", active, reserved)
			}
			unread := &uploadUnreadBody{}
			rejected := request("/uploads")
			rejected.Body = unread
			response := httptest.NewRecorder()
			kernel.ServeHTTP(response, rejected)
			if response.Code != 503 || response.Header().Get("Retry-After") != "1" || response.Header().Get("Connection") != "close" || unread.reads.Load() != 0 || middlewareCalls.Load() != 1 {
				t.Fatalf("rejection consumed work: %d, %v", response.Code, response.Header())
			}
			small := httptest.NewRecorder()
			kernel.ServeHTTP(small, request("/small"))
			if small.Code != 204 {
				t.Fatalf("small multipart blocked: %d", small.Code)
			}
			smallJSON := httptest.NewRecorder()
			kernel.ServeHTTP(smallJSON, httptest.NewRequest("GET", "/small-json", nil))
			if smallJSON.Code != 200 {
				t.Fatalf("small JSON blocked: %d", smallJSON.Code)
			}
			close(release)
			<-done
			if active, reserved := pool.Usage(); active != 0 || reserved != 0 {
				t.Fatalf("capacity retained: %d/%d", active, reserved)
			}
		})
	}
}

func TestUploadAdmissionRetainsCanceledHandlerAndCleansFailures(t *testing.T) {
	for _, failure := range []string{"cancel", "panic", "goexit", "decode"} {
		t.Run(failure, func(t *testing.T) {
			budget := DefaultEndpointLimits().Multipart.Bytes
			pool := claimedUploadTestPool(t, 1, budget)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			e := admittedEndpoint(t, pool)
			router, err := NewRouter(e.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				cancel()
				if active, reserved := pool.Usage(); active != 1 || reserved != budget {
					t.Error("cancellation released capacity before handler exit")
				}
				switch failure {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				}
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, primaryUpload("owned"))
			if failure == "decode" {
				wire = wire[:len(wire)/2]
			}
			request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire)).WithContext(ctx)
			request.Header.Set("Content-Type", media)
			router.ServeHTTP(httptest.NewRecorder(), request)
			if active, reserved := pool.Usage(); active != 0 || reserved != 0 {
				t.Fatalf("capacity not released after cleanup: %d/%d", active, reserved)
			}
			entries, err := os.ReadDir(pool.pool.Directory())
			if err != nil || len(entries) != 1 || entries[0].Name() != ".foundry-upload-owner" {
				t.Fatal("capture retained after exit")
			}
		})
	}
}

func TestUploadAdmissionValidationAndExclusiveDirectory(t *testing.T) {
	pool := claimedUploadTestPool(t, 1, DefaultEndpointLimits().Multipart.Bytes)
	e := admittedEndpoint(t, pool)
	e.route = ConfigureRoute(e.route, BodyLimit(1))
	if _, err := NewRouter(e.Handle(func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil })); err == nil {
		t.Fatal("mismatched budget accepted")
	}
	if err := ValidateMultipart(MultipartDirectory(MultipartAdmission(multipartForm(""), pool), t.TempDir())); err == nil {
		t.Fatal("directory drift accepted")
	}
	contender, err := NewUploadPool(UploadPoolConfig{Directory: pool.pool.Directory(), MaxActive: 1, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := contender.pool.Claim(); !errors.Is(err, fault.Conflict) {
		t.Fatal("active owner reused", err)
	}
	orphan := t.TempDir()
	if err := os.WriteFile(filepath.Join(orphan, "retained"), []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	unrecognized, err := NewUploadPool(UploadPoolConfig{Directory: orphan, MaxActive: 1, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := unrecognized.pool.Claim(); !errors.Is(err, fault.Invalid) {
		t.Fatal("restart ignored unknown files", err)
	}
}

func TestUploadAdmissionAppliesWithoutKernel(t *testing.T) {
	pool := claimedUploadTestPool(t, 2, DefaultEndpointLimits().Multipart.Bytes)
	e := admittedEndpoint(t, pool)
	router, err := NewRouter(e.Handle(func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	held, err := pool.pool.Reserve(t.Context(), e.limits.Multipart.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	unread := &uploadUnreadBody{}
	request := httptest.NewRequest("POST", "/uploads", nil)
	request.Body = unread
	request.ContentLength = -1
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 503 || unread.reads.Load() != 0 {
		t.Fatal("direct request bypassed byte reservation")
	}
	held.Release()
	wire, media := multipartWire(t, primaryUpload("healthy"))
	request = httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire))
	request.Header.Set("Content-Type", media)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("pool did not recover: %d", response.Code)
	}
}

func TestUploadAdmissionRejectsPrereadCapturePolicy(t *testing.T) {
	pool := claimedUploadTestPool(t, 1, DefaultEndpointLimits().Multipart.Bytes)
	router, err := NewRouter(admittedEndpoint(t, pool).Handle(func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := ParsePathPrefix("/uploads")
	if err != nil {
		t.Fatal(err)
	}
	options, err := configureServer([]ServerOption{WithWebhookCapture(captureTestRecorder(t), CapturePrefix(prefix, FullCaptureBody()))})
	if err != nil {
		t.Fatal(err)
	}
	if err := options.capture.validateRoutes(router); err == nil {
		t.Fatal("body preread accepted on admitted upload route")
	}
}

func TestUploadAdmissionRefusesHiddenKernelMetadata(t *testing.T) {
	pool := claimedUploadTestPool(t, 1, DefaultEndpointLimits().Multipart.Bytes)
	router, err := NewRouter(admittedEndpoint(t, pool).Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
		t.Error("hidden route was served")
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	hidden := stdhttp.HandlerFunc(router.ServeHTTP)
	kernel := newHandlerLifetime().wrap(hidden, slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultServerConfig())
	unread := &uploadUnreadBody{}
	request := httptest.NewRequest("POST", "/uploads", nil)
	request.Body = unread
	request.ContentLength = -1
	response := httptest.NewRecorder()
	kernel.ServeHTTP(response, request)
	if response.Code != 503 || unread.reads.Load() != 0 {
		t.Fatal("kernel accepted late upload admission")
	}
}
