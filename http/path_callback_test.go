package http

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type pathCallbackCodec struct {
	parse  func(string) (string, error)
	format func(string) (string, error)
}

func (c pathCallbackCodec) Parse(text string) (string, error)  { return c.parse(text) }
func (c pathCallbackCodec) Format(text string) (string, error) { return c.format(text) }

func callbackPathRoute(codec PathCodec[string]) Route[textPath] {
	return DefineRoute(RouteSpec{ID: "callback.show", Method: GET, Access: Public}, DefinePath("/{text}", Param("text", codec, func(p *textPath) *string { return &p.Text })))
}

func TestPathCallbackPanicAndGoexitAreInternal(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "goexit", "cancel-panic", "cancel-goexit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			method := func(string) (string, error) {
				if strings.HasPrefix(mode, "cancel-") {
					cancel()
				}
				if strings.HasSuffix(mode, "goexit") {
					runtime.Goexit()
				}
				panic("private-path-callback-detail")
			}
			route := callbackPathRoute(pathCallbackCodec{parse: method, format: method})
			router, err := NewRouter(route.HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) { t.Error("failed path reached handler") }))
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			if strings.HasSuffix(mode, "goexit") {
				// Codecs run on the request goroutine (callback.Invoke): Goexit
				// ends it like any Go call; the kernel still releases ownership.
				if !exitsGoroutine(func() { router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, "GET", "/value", nil)) }) {
					t.Fatal("path codec Goexit was converted into a response")
				}
				if !exitsGoroutine(func() { _, _ = route.URL(textPath{Text: "value"}) }) {
					t.Fatal("URL codec Goexit was converted into a return")
				}
				return
			}
			router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, "GET", "/value", nil))
			if failure := decodeFailure(t, response); failure.Code != InternalError || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("path callback response: %+v", failure)
			}
			location, err := route.URL(textPath{Text: "value"})
			if location != "" || !errors.Is(err, fault.Internal) || strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") {
				t.Fatalf("path callback URL: %q, %v", location, err)
			}
		})
	}
}

// exitsGoroutine runs fn on its own goroutine and reports whether it ended by
// runtime.Goexit rather than returning. Framework hot paths use callback.Invoke,
// so a codec's Goexit ends the calling goroutine instead of becoming an error.
func exitsGoroutine(fn func()) bool {
	returned := make(chan bool, 1)
	go func() {
		completed := false
		defer func() { returned <- completed }()
		fn()
		completed = true
	}()
	return !<-returned
}

type pathHostileError struct{ calls atomic.Int32 }

func (e *pathHostileError) fail()         { e.calls.Add(1); panic("private-path-error-method") }
func (e *pathHostileError) Error() string { e.fail(); return "" }
func (e *pathHostileError) Is(error) bool { e.fail(); return false }
func (e *pathHostileError) As(any) bool   { e.fail(); return false }
func (e *pathHostileError) Unwrap() error { e.fail(); return nil }

func TestPathCallbackDoesNotInvokeArbitraryErrorMethods(t *testing.T) {
	t.Parallel()
	cause := new(pathHostileError)
	method := func(string) (string, error) { return "partial", cause }
	route := callbackPathRoute(pathCallbackCodec{parse: method, format: method})
	location, err := route.URL(textPath{Text: "value"})
	if location != "" || !errors.Is(err, fault.Invalid) || !errors.Is(err, cause) {
		t.Fatal("URL error lost its private cause or retained partial output")
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") {
		t.Fatal("URL error exposed private data")
	}
	router, err := NewRouter(route.HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) { t.Error("failed path reached handler") }))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/value", nil))
	if failure := decodeFailure(t, response); failure.Code != BadRequest {
		t.Fatalf("codec failure classification: %+v", failure)
	}
	if cause.calls.Load() != 0 {
		t.Fatal("codec error methods ran during classification or formatting")
	}
}

func TestPathCallbackCancellationRetainsOwnership(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var laterCalls atomic.Int32
	method := func(string) (string, error) { close(entered); <-release; return "first", nil }
	type fields struct{ First, Second string }
	path := DefinePath("/{first}/{second}",
		Param("first", pathCallbackCodec{parse: method, format: method}, func(p *fields) *string { return &p.First }),
		Param("second", pathCallbackCodec{parse: func(text string) (string, error) { laterCalls.Add(1); return text, nil }, format: func(text string) (string, error) { return text, nil }}, func(p *fields) *string { return &p.Second }),
	)
	route := DefineRoute(RouteSpec{ID: "callback.cancel", Method: GET, Access: Public}, path)
	router, err := NewRouter(route.HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, fields) { t.Error("canceled path reached handler") }))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, "GET", "/one/two", nil))
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("decoder returned while callback still owned work")
	default:
	}
	close(release)
	<-done
	if laterCalls.Load() != 0 {
		t.Fatal("decoder continued into later codecs after cancellation")
	}
	if failure := decodeFailure(t, response); failure.Code != RequestTimeout {
		t.Fatalf("cancellation response: %+v", failure)
	}
}

func TestPathCallbackSkipsCanceledRequest(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var calls atomic.Int32
	method := func(text string) (string, error) { calls.Add(1); return text, nil }
	router, err := NewRouter(callbackPathRoute(pathCallbackCodec{parse: method, format: method}).HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) { t.Error("canceled request reached handler") }))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, "GET", "/value", nil))
	if calls.Load() != 0 || decodeFailure(t, response).Code != RequestTimeout {
		t.Fatal("canceled request ran a codec or lost cancellation")
	}
}

func TestPathNilSelectorURLIsInternal(t *testing.T) {
	t.Parallel()
	path := DefinePath("/{text}", Param("text", StringPath[string](), func(*textPath) *string { return nil }))
	if location, err := path.URL(textPath{Text: "value"}); location != "" || !errors.Is(err, fault.Internal) {
		t.Fatalf("nil selector: %q, %v", location, err)
	}
}
