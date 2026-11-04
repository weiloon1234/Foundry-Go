package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

type RegistryErrorDetails struct {
	ExistingRegistryID string        `json:"existing_registry_id"`
	Calls              *atomic.Int32 `json:"-"`
	Mode               string        `json:"-"`
}

func (d RegistryErrorDetails) MarshalJSON() ([]byte, error) {
	if d.Calls != nil {
		d.Calls.Add(1)
	}
	switch d.Mode {
	case "panic":
		panic("private-payload-marker")
	case "goexit":
		runtime.Goexit()
	case "failure":
		return nil, errors.New("private-payload-marker")
	case "schema":
		return []byte(`{"existing_registry_id":42}`), nil
	}
	return json.Marshal(struct {
		ID string `json:"existing_registry_id"`
	}{d.ExistingRegistryID})
}

func registryDetailsJSON() contract.JSON[RegistryErrorDetails] {
	return contract.DefineJSON[RegistryErrorDetails](contract.Schema{
		Root: "github.com/weiloon1234/Foundry-Go/http.RegistryErrorDetails",
		Types: []contract.Type{
			{ID: "github.com/weiloon1234/Foundry-Go/http.RegistryErrorDetails", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "existing_registry_id", Type: "string", Required: true}}},
			{ID: "string", Kind: contract.StringKind},
		},
	})
}

func registryError() DetailedErrorDeclaration[RegistryErrorDetails] {
	return DefineErrorDetails("registry_exists", 409, "Registry already exists.", registryDetailsJSON())
}

func detailedErrorCatalog(t *testing.T) *i18n.Catalog {
	t.Helper()
	definition, err := registryError().MessageDefinition()
	if err != nil {
		t.Fatal(err)
	}
	locales, _ := i18n.NewLocaleSet("en", "en", "ms")
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, append(MessageDefinitions(), definition), map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {
		"http.error.registry_exists": {Text: "Pendaftaran sudah wujud."},
		"http.error.internal_error":  {Text: "Ralat dalaman."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestDetailedErrorWireAndLocalization(t *testing.T) {
	declaration := registryError()
	var calls atomic.Int32
	returned := declaration.New(RegistryErrorDetails{ExistingRegistryID: "registry-123", Calls: &calls}).WithCause(privateCause{})
	router, err := NewRouter(ConfigureEndpoint(errorEndpoint("registry.create", "/registry"), Errors(declaration, DefineError("other", 410, "Gone"))).Handle(returnHTTPError(returned)))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, Locale(detailedErrorCatalog(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ locale, message string }{{"en", "Registry already exists."}, {"ms", "Pendaftaran sudah wujud."}} {
		request := httptest.NewRequest("GET", "/registry", nil)
		request.Header.Set("Accept-Language", tc.locale)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := `{"status":409,"error_code":"registry_exists","message":"` + tc.message + `","details":{"existing_registry_id":"registry-123"}}` + "\n"
		if response.Code != 409 || response.Body.String() != want {
			t.Fatalf("wire: %d %s; want %s", response.Code, response.Body, want)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("marshaled %d times", calls.Load())
	}
	info := router.Endpoints()[0].Errors[0]
	schema, err := router.ErrorDetailsSchema(info.Code)
	if err != nil || info.DetailsType != schema.Root {
		t.Fatal("lost details schema", info, err)
	}
	schema.Types[0].ID = "mutated"
	again, _ := router.ErrorDetailsSchema(info.Code)
	if again.Types[0].ID == "mutated" {
		t.Fatal("schema snapshot aliases router")
	}
}

func TestDetailedErrorFailuresAndProbe(t *testing.T) {
	declared := registryError()
	changedSchema, _ := registryDetailsJSON().Description()
	for i := range changedSchema.Types {
		if changedSchema.Types[i].Kind == contract.StringKind {
			changedSchema.Types[i].Cases = []json.RawMessage{json.RawMessage(`"private-payload-marker"`)}
		}
	}
	conflicting := DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](changedSchema))
	different := DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.StringJSON[string]())
	for _, tc := range []struct {
		name        string
		declaration ErrorDeclarationOption
		mode        string
		limit       string
		returned    func(*DetailedError) error
		status      int
		calls       int32
	}{
		{name: "undeclared", status: 500},
		{name: "plain same triple", declaration: DefineError("registry_exists", 409, "Registry already exists."), status: 500},
		{name: "different D", declaration: different, status: 500},
		{name: "conflicting schema", declaration: conflicting, status: 500},
		{name: "encode failure", declaration: declared, mode: "failure", status: 500, calls: 1},
		{name: "encode panic", declaration: declared, mode: "panic", status: 500, calls: 1},
		{name: "encode goexit", declaration: declared, mode: "goexit", status: 500, calls: 1},
		{name: "schema failure", declaration: declared, mode: "schema", status: 500, calls: 1},
		{name: "envelope bytes", declaration: declared, limit: "bytes", status: 500, calls: 1},
		{name: "envelope depth", declaration: declared, limit: "depth", status: 500, calls: 1},
		{name: "envelope nodes", declaration: declared, limit: "nodes", status: 500, calls: 1},
		{name: "outer conflict", declaration: declared, returned: func(e *DetailedError) error { return Conflict.WithCause(e) }, status: 409},
	} {
		for _, probe := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/probe=%v", tc.name, probe), func(t *testing.T) {
				var calls atomic.Int32
				failure := declared.New(RegistryErrorDetails{ExistingRegistryID: "private-payload-marker", Calls: &calls, Mode: tc.mode}).WithCause(privateCause{})
				var returned error = failure
				if tc.returned != nil {
					returned = tc.returned(failure)
				}
				endpoint := errorEndpoint("registry.create", "/registry")
				if tc.declaration != nil {
					endpoint = ConfigureEndpoint(endpoint, Errors(tc.declaration))
				}
				limits := DefaultEndpointLimits()
				switch tc.limit {
				case "bytes":
					limits.Response.Bytes = 100
				case "depth":
					limits.Response.Depth = 1
				case "nodes":
					limits.Response.Nodes = 5
				}
				endpoint = ConfigureEndpoint(endpoint, Limits(limits))
				if probe {
					endpoint = ConfigureEndpoint(endpoint, Use(DefineMiddleware("probe", func(next stdhttp.Handler) (stdhttp.Handler, error) {
						return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
							_, _ = localizedErrorResponse(r.Context(), returned, nil)
							if calls.Load() != 0 {
								t.Error("probe encoded payload")
							}
							next.ServeHTTP(w, r)
						}), nil
					})))
				}
				router, err := NewRouter(endpoint.Handle(returnHTTPError(returned)), errorEndpoint("healthy", "/healthy").Handle(returnHTTPError(nil)))
				if err != nil {
					t.Fatal(err)
				}
				handler, err := ApplyMiddleware(router, Locale(detailedErrorCatalog(t)))
				if err != nil {
					t.Fatal(err)
				}
				var logs bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logs, nil))
				req := withRequestLogger(httptest.NewRequest("GET", "/registry", nil), logger)
				req.Header.Set("Accept-Language", "ms")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				expected := `{"status":500,"error_code":"internal_error","message":"Ralat dalaman."}` + "\n"
				if tc.status == 409 {
					expected = `{"status":409,"error_code":"conflict","message":"Request conflicts with current state"}` + "\n"
				}
				if response.Code != tc.status || response.Body.String() != expected {
					t.Fatalf("wire: %d %s; want %s", response.Code, response.Body, expected)
				}
				if calls.Load() != tc.calls {
					t.Fatalf("marshaled %d times, want %d", calls.Load(), tc.calls)
				}
				reports := 0
				if tc.status == 500 {
					reports = 1
				}
				if strings.Count(logs.String(), "HTTP request failed") != reports {
					t.Fatalf("reports: %s", &logs)
				}
				if strings.Contains(logs.String(), "private-payload-marker") || strings.Contains(logs.String(), "hunter2") {
					t.Fatal("diagnostic exposed private data", &logs)
				}
				healthy := httptest.NewRecorder()
				handler.ServeHTTP(healthy, httptest.NewRequest("GET", "/healthy", nil))
				if healthy.Code != 204 || healthy.Body.Len() != 0 {
					t.Fatal("healthy request failed", healthy.Code, healthy.Body)
				}
			})
		}
	}
}

func TestDetailedErrorRawAndUnmatchedFailClosed(t *testing.T) {
	var calls atomic.Int32
	failure := registryError().New(RegistryErrorDetails{ExistingRegistryID: "private-payload-marker", Calls: &calls}).WithCause(privateCause{})
	router, err := NewRouter(HandleRaw(DefineRoute(RouteSpec{ID: "raw", Method: GET, Access: Public}, StaticPath("/raw")), func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) { _ = WriteError(w, r, failure) }), errorEndpoint("healthy", "/healthy").Handle(returnHTTPError(nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/raw", "/unmatched"} {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		response := httptest.NewRecorder()
		request := withRequestLogger(httptest.NewRequest("GET", path, nil), logger)
		handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if r.URL.Path == "/unmatched" {
				_ = WriteError(w, r, failure)
				return
			}
			router.ServeHTTP(w, r)
		})
		handler.ServeHTTP(response, request)
		if response.Code != 500 || response.Body.String() != `{"status":500,"error_code":"internal_error","message":"Internal server error"}`+"\n" {
			t.Fatal("details escaped", response.Code, response.Body)
		}
		if calls.Load() != 0 || strings.Count(logs.String(), "HTTP request failed") != 1 || strings.Contains(logs.String(), "hunter2") || strings.Contains(logs.String(), "private-payload-marker") {
			t.Fatal("unsafe raw failure", calls.Load(), &logs)
		}
		healthy := httptest.NewRecorder()
		handler.ServeHTTP(healthy, httptest.NewRequest("GET", "/healthy", nil))
		if healthy.Code != 204 {
			t.Fatal(healthy.Code)
		}
	}
}

func TestDetailedErrorCancelledContextAndFormatting(t *testing.T) {
	declaration := registryError()
	cause := privateCause{}
	failure := declaration.New(RegistryErrorDetails{ExistingRegistryID: "private-payload-marker"}).WithCause(cause)
	for _, format := range []string{"%v", "%+v", "%#v", "%d", "%s"} {
		if got := fmt.Sprintf(format, failure); got != "registry_exists" {
			t.Fatalf("unsafe format %s: %s", format, got)
		}
	}
	var found privateCause
	if !errors.Is(failure, cause) || !errors.As(failure, &found) {
		t.Fatal("lost cause identity")
	}
	for _, expired := range []bool{false, true} {
		var cancel context.CancelFunc
		router, err := NewRouter(ConfigureEndpoint(errorEndpoint("registry.create", "/registry"), Errors(declaration)).Handle(func(ctx context.Context, _ Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
			if expired {
				<-ctx.Done()
			} else {
				cancel()
			}
			want := context.Canceled
			if expired {
				want = context.DeadlineExceeded
			}
			if ctx.Err() != want {
				t.Errorf("handler context: %v", ctx.Err())
			}
			return NoContent{}, failure
		}))
		if err != nil {
			t.Fatal(err)
		}
		var ctx context.Context
		if expired {
			ctx, cancel = context.WithTimeout(t.Context(), time.Second)
		} else {
			ctx, cancel = context.WithCancel(t.Context())
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/registry", nil).WithContext(ctx))
		cancel()
		if response.Code != 409 || !strings.Contains(response.Body.String(), `"details":{"existing_registry_id":"private-payload-marker"}`) {
			t.Fatal("cancelled response downgraded", response.Code, response.Body)
		}
	}

}

func TestDetailedErrorAssemblyAndRouterIsolation(t *testing.T) {
	declaration := registryError()
	schema, _ := registryDetailsJSON().Description()
	for i := range schema.Types {
		if schema.Types[i].Kind == contract.ObjectKind {
			schema.Types[i].Properties[0].Presentation.Kind = contract.PasswordPresentation
		}
	}
	password := DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](schema))
	for _, d := range []ErrorDeclarationOption{DetailedErrorDeclaration[RegistryErrorDetails]{}, DefineErrorDetails("registry_exists", 401, "Registry already exists.", registryDetailsJSON()), password} {
		if router, err := NewRouter(ConfigureEndpoint(errorEndpoint("bad", "/bad"), Errors(d)).Handle(returnHTTPError(nil))); err == nil || router != nil {
			t.Fatal("invalid declaration assembled")
		}
	}
	head := DefineEndpoint(DefineRoute(RouteSpec{ID: "head", Method: HEAD, Access: Public}, StaticPath("/head")), EmptyQuery(), EmptyBody(), EmptyResponse(204), Errors(declaration))
	if router, err := NewRouter(head.Handle(returnHTTPError(nil))); err == nil || router != nil {
		t.Fatal("HEAD details assembled")
	}
	schema, _ = registryDetailsJSON().Description()
	for i := range schema.Types {
		if schema.Types[i].Kind == contract.StringKind {
			schema.Types[i].Cases = []json.RawMessage{json.RawMessage(`"registry-123"`)}
		}
	}
	changed := DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](schema))
	register := func(id RouteID, path string, d ErrorDeclarationOption, returned error) RouteRegistration {
		return ConfigureEndpoint(errorEndpoint(id, path), Errors(d)).Handle(returnHTTPError(returned))
	}
	for _, other := range []ErrorDeclarationOption{changed, DefineError("registry_exists", 409, "Registry already exists."), DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.StringJSON[string]())} {
		if router, err := NewRouter(register("a", "/a", declaration, nil), register("b", "/b", other, nil)); !errors.Is(err, fault.Conflict) || router != nil {
			t.Fatal("conflicting details assembled", err)
		}
	}
	a, err := NewRouter(register("a", "/a", declaration, declaration.New(RegistryErrorDetails{ExistingRegistryID: "another"})))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRouter(register("b", "/b", changed, changed.New(RegistryErrorDetails{ExistingRegistryID: "registry-123"})))
	if err != nil {
		t.Fatal("separate router shared catalog", err)
	}
	for _, tc := range []struct {
		router   *Router
		path, id string
	}{{a, "/a", "another"}, {b, "/b", "registry-123"}} {
		response := httptest.NewRecorder()
		tc.router.ServeHTTP(response, httptest.NewRequest("GET", tc.path, nil))
		if response.Code != 409 || !strings.Contains(response.Body.String(), tc.id) {
			t.Fatal("router isolation failed", response.Code, response.Body)
		}
	}
	snapshot := a.ErrorDefinitions()
	snapshot[0].Message = "mutated"
	if a.ErrorDefinitions()[0].Message == "mutated" {
		t.Fatal("router catalog aliases caller")
	}
}

func TestDetailedErrorPlainBytesUnchanged(t *testing.T) {
	for _, tc := range []struct {
		failure error
		body    string
	}{
		{Conflict, `{"status":409,"error_code":"conflict","message":"Request conflicts with current state"}`},
		{DefineError("registry_exists", 409, "Registry already exists."), `{"status":409,"error_code":"registry_exists","message":"Registry already exists."}`},
	} {
		declaration := DefineError("registry_exists", 409, "Registry already exists.")
		router, err := NewRouter(ConfigureEndpoint(errorEndpoint("plain", "/plain"), Errors(declaration)).Handle(returnHTTPError(tc.failure)))
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/plain", nil))
		if response.Code != 409 || response.Body.String() != tc.body+"\n" {
			t.Fatal("plain wire changed", response.Code, response.Body)
		}
	}
}
