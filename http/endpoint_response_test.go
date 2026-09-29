package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

type EndpointCodecReply struct{ encode func() ([]byte, error) }

func (r EndpointCodecReply) MarshalJSON() ([]byte, error) { return r.encode() }

func TestTypedEndpointPreparesResponseBeforeCommit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"schema", "malformed", "panic", "goexit", "error", "cancel", "cancel-panic", "limit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			descriptor := endpointDTO[EndpointCodecReply](contract.Property{Name: "name", Type: "text", Required: true})
			e := DefineEndpoint(DefineRoute(RouteSpec{ID: "reply", Method: GET, Access: Public}, StaticPath("/")), EmptyQuery(), EmptyBody(), JSONResponse(201, descriptor))
			if mode == "limit" {
				limits := DefaultEndpointLimits()
				limits.Response.Bytes = 8
				e = e.WithLimits(limits)
			}
			w := httptest.NewRecorder()
			router, err := NewRouter(e.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (EndpointCodecReply, error) {
				return EndpointCodecReply{encode: func() ([]byte, error) {
					if w.Flushed || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
						t.Error("response committed before codec finished")
					}
					switch mode {
					case "schema":
						return []byte(`{"name":12}`), nil
					case "malformed":
						return []byte(`{"name":`), nil
					case "panic":
						panic("private-codec-detail")
					case "goexit":
						runtime.Goexit()
					case "error":
						return nil, errors.New("private-codec-detail")
					case "cancel":
						cancel()
					case "cancel-panic":
						cancel()
						panic("private-codec-detail")
					}
					return []byte(`{"name":"private-response-value"}`), nil
				}}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			router.ServeHTTP(w, httptest.NewRequestWithContext(ctx, "GET", "/", nil))
			if mode == "cancel" {
				// Encoding after a successful handler is detached from
				// cancellation: the completed success is published.
				if w.Code != 201 {
					t.Fatalf("completed success replaced after cancellation: %d %s", w.Code, w.Body.String())
				}
				return
			}
			want := 500
			failure := decodeFailure(t, w)
			if w.Code != want || len(failure.Issues) != 0 || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("invalid response escaped: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

type endpointShortWriter struct {
	header   stdhttp.Header
	statuses []int
	writes   int
}

func (w *endpointShortWriter) Header() stdhttp.Header         { return w.header }
func (w *endpointShortWriter) WriteHeader(status int)         { w.statuses = append(w.statuses, status) }
func (w *endpointShortWriter) Write(data []byte) (int, error) { w.writes++; return len(data) - 1, nil }

func TestTypedEndpointAbortsAfterShortNativeWrite(t *testing.T) {
	t.Parallel()
	e := DefineEndpoint(DefineRoute(RouteSpec{ID: "reply", Method: GET, Access: Public}, StaticPath("/")), EmptyQuery(), EmptyBody(), JSONResponse(201, endpointReplyJSON()))
	router, err := NewRouter(e.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (EndpointReply, error) {
		return EndpointReply{Name: "ok"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	w := &endpointShortWriter{header: make(stdhttp.Header)}
	defer func() {
		if recovered := recover(); recovered != stdhttp.ErrAbortHandler {
			t.Errorf("short write did not abort: %v", recovered)
		}
		if len(w.statuses) != 1 || w.statuses[0] != 201 || w.writes != 1 {
			t.Errorf("attempted replacement after commit: %+v", w)
		}
	}()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
}

func TestTypedEndpointMediaAndEarlyFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		media      []string
		want       int
		unread     bool
	}{
		{"vendor", "/items/" + endpointUserID, []string{"application/vnd.example+json"}, 201, false},
		{"duplicate", "/items/" + endpointUserID, []string{"application/json", "application/json"}, 415, true},
		{"query-first", "/items/" + endpointUserID + "?q=a&q=b", []string{"application/json"}, 400, true},
		{"path-first", "/items/invalid", []string{"application/json"}, 400, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(`{"name":"ok"}`)
			body := &endpointReader{read: reader.Read}
			router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) { return EndpointReply{Name: "ok"}, nil }))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("PATCH", tc.path, nil)
			r.Body = body
			r.ContentLength = -1
			r.Header["Content-Type"] = tc.media
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want || tc.unread && body.calls.Load() != 0 {
				t.Fatalf("media/input ordering: status=%d reads=%d", w.Code, body.calls.Load())
			}
		})
	}
}
