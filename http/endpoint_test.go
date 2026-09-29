package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type EndpointPatch struct {
	Name string                                 `json:"name"`
	Note value.Optional[value.Nullable[string]] `json:"note,omitzero"`
}
type EndpointReply struct {
	ID    string `json:"id"`
	Query string `json:"query"`
	Name  string `json:"name"`
	Null  bool   `json:"null"`
}
type endpointParameters struct{ Term value.Optional[string] }
type endpointRequest = Input[userPath, endpointParameters, EndpointPatch]

func endpointDTO[T any](properties ...contract.Property) contract.JSON[T] {
	typ := reflect.TypeFor[T]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[T](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: properties},
		{ID: "text", Kind: contract.StringKind},
		{ID: "nullable_text", Kind: contract.StringKind, Nullable: true},
		{ID: "bool", Kind: contract.BooleanKind},
	}})
}

func endpointPatchJSON() contract.JSON[EndpointPatch] {
	return endpointDTO[EndpointPatch](contract.Property{Name: "name", Type: "text", Required: true}, contract.Property{Name: "note", Type: "nullable_text"})
}
func endpointReplyJSON() contract.JSON[EndpointReply] {
	return endpointDTO[EndpointReply](contract.Property{Name: "id", Type: "text", Required: true}, contract.Property{Name: "query", Type: "text", Required: true}, contract.Property{Name: "name", Type: "text", Required: true}, contract.Property{Name: "null", Type: "bool", Required: true})
}

func patchEndpoint() Endpoint[userPath, endpointParameters, EndpointPatch, EndpointReply] {
	route := DefineRoute(RouteSpec{ID: "items.update", Method: PATCH, Access: Public}, DefinePath("/items/{user}", Param("user", ModelIDPath[routeUser](), func(p *userPath) *model.ID[routeUser] { return &p.User })))
	query := DefineQuery(OptionalQueryParam("q", StringQuery[string](), func(q *endpointParameters) *value.Optional[string] { return &q.Term }))
	return DefineEndpoint(route, query, JSONBody(endpointPatchJSON()), JSONResponse(201, endpointReplyJSON()))
}

const endpointUserID = "0193fd8c-2075-7000-8000-000000000001"

func TestTypedEndpointInputAndMetadata(t *testing.T) {
	t.Parallel()
	endpoint := patchEndpoint().Within(DefineScope("/api", "api"))
	var called atomic.Int32
	router, err := NewRouter(endpoint.Handle(func(ctx context.Context, input endpointRequest) (EndpointReply, error) {
		called.Add(1)
		info, ok := MatchedRoute(ctx)
		if !ok || info.Raw || info.ID != "api.items.update" {
			t.Error("typed matched route metadata missing")
		}
		term, _ := input.Query.Term.Get()
		note, set := input.Body.Note.Get()
		return EndpointReply{ID: input.Path.User.String(), Query: term, Name: input.Body.Name, Null: set && note.IsNull()}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[routeUser](endpointUserID)
	if err != nil {
		t.Fatal(err)
	}
	location, err := endpoint.URL(t.Context(), userPath{User: id}, endpointParameters{Term: value.Set("query + value")})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PATCH", location, strings.NewReader(`{"name":"body value","note":null}`))
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var got EndpointReply
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != 201 || called.Load() != 1 || got.ID != endpointUserID || got.Query != "query + value" || got.Name != "body value" || !got.Null {
		t.Fatalf("typed response: %d %+v", response.Code, got)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Content-Length") == "" {
		t.Fatal("response representation metadata missing")
	}
	entries := router.Endpoints()
	if len(entries) != 1 || entries[0].Body == nil || entries[0].Response == nil || entries[0].Status != 201 || entries[0].Query[0].Name != "q" {
		t.Fatal("endpoint contract metadata missing")
	}
	entries[0].Body.Schema.Types[0].ID = "changed"
	entries[0].Route.Parameters[0] = "changed"
	entries[0].Query[0].Name = "changed"
	again := router.Endpoints()
	if again[0].Body.Schema.Types[0].ID == "changed" || again[0].Route.Parameters[0] == "changed" || again[0].Query[0].Name == "changed" {
		t.Fatal("endpoint metadata was not owned")
	}
	if router.Routes()[0].Raw {
		t.Fatal("typed endpoint marked raw")
	}
}

func TestTypedEndpointInputFailuresDoNotCallHandler(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, body, media, encoding, issue string
		status                                   int
	}{
		{"path", "/items/bad", `{"name":"ok"}`, "application/json", "", "/path/user", 400},
		{"query", "/items/" + endpointUserID + "?q=a&%71=b", `{"name":"ok"}`, "application/json", "", "/query/q", 400},
		{"unknown-query", "/items/" + endpointUserID + "?private-key=value", `{"name":"ok"}`, "application/json", "", "/query", 400},
		{"body-type", "/items/" + endpointUserID, `{"name":12}`, "application/json", "", "/body/name", 400},
		{"body-required", "/items/" + endpointUserID, `{}`, "application/json", "", "/body/name", 400},
		{"body-unknown", "/items/" + endpointUserID, `{"name":"ok","private-key":"secret"}`, "application/json", "", "/body", 400},
		{"malformed", "/items/" + endpointUserID, `{"name":`, "application/json", "", "", 400},
		{"media", "/items/" + endpointUserID, `{"name":"ok"}`, "text/plain", "", "", 415},
		{"charset", "/items/" + endpointUserID, `{"name":"ok"}`, "application/json;charset=latin1", "", "", 415},
		{"encoding", "/items/" + endpointUserID, `{"name":"ok"}`, "application/json", "gzip", "", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
				t.Error("invalid input reached handler")
				return EndpointReply{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("PATCH", tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.media)
			if tc.encoding != "" {
				request.Header.Set("Content-Encoding", tc.encoding)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			failure := decodeFailure(t, response)
			if response.Code != tc.status {
				t.Fatalf("status %d: %+v", response.Code, failure)
			}
			if tc.issue != "" && (len(failure.Issues) != 1 || failure.Issues[0].Path != tc.issue) {
				t.Fatalf("issues: %+v", failure.Issues)
			}
			if strings.Contains(response.Body.String(), "private-key") || strings.Contains(response.Body.String(), "secret") {
				t.Fatal("input detail reflected into errors")
			}
		})
	}
}

type endpointReader struct {
	read  func([]byte) (int, error)
	calls atomic.Int32
}

func (r *endpointReader) Read(data []byte) (int, error) { r.calls.Add(1); return r.read(data) }
func (*endpointReader) Close() error                    { return nil }

func TestTypedEndpointBoundsKnownAndStreamedBodies(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "known", false: "streamed"}[known], func(t *testing.T) {
			limits := DefaultEndpointLimits()
			limits.Body.Bytes = 16
			router, err := NewRouter(patchEndpoint().WithLimits(limits).Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
				t.Error("oversized input reached handler")
				return EndpointReply{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			reader := &endpointReader{read: func(data []byte) (int, error) {
				for i := range data {
					data[i] = 'x'
				}
				return len(data), nil
			}}
			request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, nil)
			request.Body = reader
			request.ContentLength = -1
			if known {
				request.ContentLength = 1000
			}
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 413 || decodeFailure(t, response).Code != PayloadTooLarge || response.Header().Get("Connection") != "close" {
				t.Fatal("body ceiling did not reject safely")
			}
			if known && reader.calls.Load() != 0 {
				t.Fatal("known oversize body was read")
			}
		})
	}
}

func TestTypedEndpointHandlerFailureAndCancellation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "goexit", "returned", "cancel-panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
				if mode == "returned" {
					return EndpointReply{}, Forbidden.WithCause(errors.New("private-handler-detail"))
				}
				if mode == "cancel-panic" {
					cancel()
				}
				if mode == "goexit" {
					runtime.Goexit()
				}
				panic("private-handler-detail")
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequestWithContext(ctx, "PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"ok"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := 500
			if mode == "returned" {
				want = 403
			}
			if response.Code != want || strings.Contains(response.Body.String(), "private-handler-detail") {
				t.Fatalf("handler failure: %d %s", response.Code, response.Body.String())
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		close(entered)
		<-release
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(ctx, "PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"ok"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); router.ServeHTTP(response, request) }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("handler was abandoned on cancellation")
	default:
	}
	close(release)
	<-done
	// The handler returned success after cancellation: its completed outcome
	// is published, never replaced by a timeout.
	if response.Code != 201 {
		t.Fatalf("canceled handler success: %d", response.Code)
	}
}

func TestTypedEndpointEmptyBodyAndHead(t *testing.T) {
	t.Parallel()
	for _, status := range []int{204, 205} {
		e := DefineEndpoint(DefineRoute(RouteSpec{ID: "empty", Method: DELETE, Access: Public}, StaticPath("/")), EmptyQuery(), EmptyBody(), EmptyResponse(status))
		router, err := NewRouter(e.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) { return NoContent{}, nil }))
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("DELETE", "/", nil))
		if response.Code != status || response.Body.Len() != 0 {
			t.Fatal("empty response emitted content")
		}
		if status == 205 && response.Header().Get("Content-Length") != "0" {
			t.Fatal("reset response length missing")
		}
		for _, known := range []bool{true, false} {
			request := httptest.NewRequest("DELETE", "/", strings.NewReader("x"))
			if !known {
				request.ContentLength = -1
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 400 {
				t.Fatal("empty body accepted request content")
			}
		}
	}
	e := DefineEndpoint(DefineRoute(RouteSpec{ID: "head", Method: GET, Access: Public}, StaticPath("/")), EmptyQuery(), EmptyBody(), JSONResponse(200, endpointReplyJSON()))
	router, err := NewRouter(e.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (EndpointReply, error) {
		return EndpointReply{Name: "ok"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	get, head := httptest.NewRecorder(), httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest("GET", "/", nil))
	router.ServeHTTP(head, httptest.NewRequest("HEAD", "/", nil))
	if get.Code != 200 || head.Code != 200 || head.Body.Len() != 0 || get.Header().Get("Content-Length") != head.Header().Get("Content-Length") {
		t.Fatal("HEAD representation disagrees with GET")
	}
}

func TestTypedEndpointRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()
	var undefined Endpoint[NoPath, NoQuery, NoBody, NoContent]
	if undefined.Validate() == nil {
		t.Fatal("zero endpoint accepted")
	}
	if _, err := NewRouter(patchEndpoint().Handle(nil)); err == nil {
		t.Fatal("nil handler accepted")
	}
	if patchEndpoint().WithLimits(EndpointLimits{}).Validate() == nil {
		t.Fatal("zero limits accepted")
	}
	for _, status := range []int{0, 101, 204, 205, 400, 600} {
		if JSONResponse(status, endpointReplyJSON()).Validate() == nil {
			t.Fatal("invalid JSON response status accepted")
		}
	}
	for _, status := range []int{0, 200, 304, 500} {
		if EmptyResponse(status).Validate() == nil {
			t.Fatal("invalid empty status accepted")
		}
	}
	get := DefineEndpoint(DefineRoute(RouteSpec{ID: "get", Method: GET, Access: Public}, StaticPath("/")), EmptyQuery(), JSONBody(endpointPatchJSON()), JSONResponse(200, endpointReplyJSON()))
	if get.Validate() == nil {
		t.Fatal("typed GET body accepted")
	}
}

func TestTypedEndpointReaderFailureIsOwned(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "goexit", "error"} {
		t.Run(mode, func(t *testing.T) {
			reader := &endpointReader{read: func([]byte) (int, error) {
				switch mode {
				case "panic":
					panic("private-reader-detail")
				case "goexit":
					runtime.Goexit()
				}
				return 0, io.ErrUnexpectedEOF
			}}
			router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
				t.Error("failed reader reached handler")
				return EndpointReply{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, nil)
			request.Body = reader
			request.ContentLength = -1
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			if mode == "goexit" {
				// Body reads run on the request goroutine (callback.Invoke).
				if !exitsGoroutine(func() { router.ServeHTTP(response, request) }) {
					t.Fatal("reader Goexit was converted into a response")
				}
				return
			}
			router.ServeHTTP(response, request)
			want := 500
			if mode == "error" {
				want = 400
			}
			if response.Code != want || strings.Contains(response.Body.String(), "private") {
				t.Fatal("reader failure escaped recovery")
			}
		})
	}
}
