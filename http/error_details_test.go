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
	"slices"
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
				for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
					if len(line) == 0 {
						continue
					}
					var report struct {
						Message    string    `json:"msg"`
						Route      RouteID   `json:"route_id"`
						Status     int       `json:"status"`
						Code       ErrorCode `json:"error_code"`
						Diagnostic struct {
							Types  []string `json:"types"`
							Faults []string `json:"faults"`
						} `json:"diagnostic"`
					}
					if err := json.Unmarshal(line, &report); err != nil {
						t.Fatal(err)
					}
					if report.Message != "HTTP request failed" {
						continue
					}
					if report.Route != "registry.create" || report.Status != 500 || report.Code != InternalError || len(report.Diagnostic.Types) == 0 {
						t.Fatalf("incorrect failure report: %+v", report)
					}
					if tc.calls == 1 {
						if !slices.Contains(report.Diagnostic.Faults, "internal: typed HTTP error details failed their contract or EndpointLimits.Response bounds") {
							t.Fatalf("report lost preparation failure: %+v", report.Diagnostic)
						}
					} else if !slices.Contains(report.Diagnostic.Types, "*http.DetailedError") {
						t.Fatalf("report lost returned failure: %+v", report.Diagnostic)
					}
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

type embeddedDetailedError struct{ *DetailedError }
type embeddedDetailedErrorValue struct{ DetailedError }

func TestDetailedErrorEmbeddingAndMissingEncoder(t *testing.T) {
	declaration := registryError()
	var calls atomic.Int32
	failure := declaration.New(RegistryErrorDetails{ExistingRegistryID: "registry-123", Calls: &calls})
	for _, tc := range []struct {
		name     string
		returned error
		bound    bool
	}{
		{"embedded pointer", embeddedDetailedError{failure}, true},
		{"pointer to embedded pointer", &embeddedDetailedError{failure}, true},
		{"embedded value", &embeddedDetailedErrorValue{*failure}, true},
		{"embedded nil", embeddedDetailedError{}, false},
		{"missing encoder", ErrorDeclaration{definition: declaration.definition}, false},
	} {
		for _, declared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/declared=%v", tc.name, declared), func(t *testing.T) {
				endpoint := errorEndpoint("registry.create", "/registry")
				if declared {
					endpoint = ConfigureEndpoint(endpoint, Errors(declaration))
				}
				router, err := NewRouter(endpoint.Handle(returnHTTPError(tc.returned)))
				if err != nil {
					t.Fatal(err)
				}
				calls.Store(0)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("GET", "/registry", nil))
				want := `{"status":500,"error_code":"internal_error","message":"Internal server error"}` + "\n"
				status, encodes := 500, int32(0)
				if declared && tc.bound {
					want = `{"status":409,"error_code":"registry_exists","message":"Registry already exists.","details":{"existing_registry_id":"registry-123"}}` + "\n"
					status, encodes = 409, 1
				}
				if response.Code != status || response.Body.String() != want || calls.Load() != encodes {
					t.Fatalf("wire: %d %s; encodes: %d", response.Code, response.Body, calls.Load())
				}
			})
		}
	}
}

func TestInvalidDetailedErrorsFailClosedOnTypedAndRawRoutes(t *testing.T) {
	var typedNil *DetailedError
	for _, tc := range []struct {
		name     string
		returned error
	}{
		{"invalid status", DefineErrorDetails("registry_exists", 401, "Registry already exists.", registryDetailsJSON()).New(RegistryErrorDetails{})},
		{"zero declaration", (DetailedErrorDeclaration[RegistryErrorDetails]{}).New(RegistryErrorDetails{})},
		{"typed nil", typedNil},
	} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw=%v", tc.name, raw), func(t *testing.T) {
				registration := ConfigureEndpoint(errorEndpoint("invalid", "/invalid"), Errors(registryError())).Handle(returnHTTPError(tc.returned))
				if raw {
					registration = HandleRaw(DefineRoute(RouteSpec{ID: "invalid", Method: GET, Access: Public}, StaticPath("/invalid")), func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
						if err := WriteError(w, r, tc.returned); err != nil {
							t.Error(err)
						}
					})
				}
				router, err := NewRouter(registration)
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("GET", "/invalid", nil))
				if response.Code != 500 || response.Body.String() != `{"status":500,"error_code":"internal_error","message":"Internal server error"}`+"\n" {
					t.Fatal("invalid details escaped", response.Code, response.Body)
				}
			})
		}
	}
}

func TestDetailedErrorNilReceiver(t *testing.T) {
	var failure *DetailedError
	if failure.Unwrap() != nil || failure.WithCause(privateCause{}) != nil || errors.Is(failure, privateCause{}) {
		t.Fatal("nil receiver acquired a cause")
	}
}

func TestErrorOptionsRejectNilDeclarationsAtAssembly(t *testing.T) {
	for _, declaration := range []ErrorDeclarationOption{nil, (*ErrorDeclaration)(nil), (*DetailedErrorDeclaration[RegistryErrorDetails])(nil)} {
		option := Errors(declaration)
		if router, err := NewRouter(ConfigureEndpoint(errorEndpoint("invalid", "/invalid"), option).Handle(returnHTTPError(nil))); !errors.Is(err, fault.Invalid) || router != nil {
			t.Fatal("nil declaration assembled", err)
		}
	}
	plain, detailed := DefineError("other", 410, "Gone"), registryError()
	if router, err := NewRouter(ConfigureEndpoint(errorEndpoint("valid", "/valid"), Errors(&plain, &detailed)).Handle(returnHTTPError(nil))); err != nil || router == nil {
		t.Fatal("valid pointer declarations rejected", err)
	}
}

func TestDetailedErrorDeclarationMatches(t *testing.T) {
	declaration := registryError()
	failure := declaration.New(RegistryErrorDetails{ExistingRegistryID: "registry-123"})
	schema, _ := registryDetailsJSON().Description()
	for i := range schema.Types {
		if schema.Types[i].Kind == contract.StringKind {
			schema.Types[i].Cases = []json.RawMessage{json.RawMessage(`"registry-123"`)}
		}
	}
	differentSchema := DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](schema))
	cycle := &classificationCallbackError{}
	cycle.unwrap = func() error { return cycle }
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"direct", failure, true},
		{"cause retained", failure.WithCause(privateCause{}), true},
		{"wrapped", fmt.Errorf("wrapped: %w", failure), true},
		{"joined", errors.Join(Forbidden, failure), true},
		{"in cause", Conflict.WithCause(failure), true},
		{"embedded", embeddedDetailedError{failure}, true},
		{"different definition", DefineErrorDetails("registry_exists", 410, "Gone", registryDetailsJSON()).New(RegistryErrorDetails{}), false},
		{"different fingerprint", differentSchema.New(RegistryErrorDetails{ExistingRegistryID: "registry-123"}), false},
		{"plain same code", DefineError("registry_exists", 409, "Registry already exists."), false},
		{"nil", nil, false},
		{"typed nil", (*DetailedError)(nil), false},
		{"cycle", cycle, false},
		{"panic", &classificationCallbackError{unwrap: func() error { panic("private") }}, false},
		{"goexit", &classificationCallbackError{unwrap: func() error { runtime.Goexit(); return nil }}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := declaration.Matches(tc.err); got != tc.want {
				t.Fatalf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
	for _, invalid := range []DetailedErrorDeclaration[RegistryErrorDetails]{{}, DefineErrorDetails("registry_exists", 401, "Invalid", registryDetailsJSON())} {
		if invalid.Matches(failure) {
			t.Fatal("invalid declaration matched")
		}
	}
}

func TestErrorLocalizationAfterContextEnds(t *testing.T) {
	for _, detailed := range []bool{false, true} {
		for _, end := range []string{"healthy", "cancelled", "expired"} {
			t.Run(fmt.Sprintf("detailed=%v/%s", detailed, end), func(t *testing.T) {
				declaration := registryError()
				var option ErrorDeclarationOption = declaration
				var failure error = declaration.New(RegistryErrorDetails{ExistingRegistryID: "registry-123"})
				if !detailed {
					plain := DefineError("registry_exists", 409, "Registry already exists.")
					option, failure = plain, plain
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				router, err := NewRouter(ConfigureEndpoint(errorEndpoint("registry.create", "/registry"), Errors(option)).Handle(func(ctx context.Context, _ Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
					if end == "cancelled" {
						cancel()
					} else if end == "expired" {
						<-ctx.Done()
					}
					if end != "healthy" && ctx.Err() == nil {
						t.Error("context still live")
					}
					return NoContent{}, failure
				}))
				if err != nil {
					t.Fatal(err)
				}
				handler, err := ApplyMiddleware(router, Locale(detailedErrorCatalog(t)))
				if err != nil {
					t.Fatal(err)
				}
				if end == "expired" {
					var expire context.CancelFunc
					ctx, expire = context.WithTimeout(ctx, 10*time.Millisecond)
					defer expire()
				}
				request := httptest.NewRequestWithContext(ctx, "GET", "/registry", nil)
				request.Header.Set("Accept-Language", "ms")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				want := `{"status":409,"error_code":"registry_exists","message":"Pendaftaran sudah wujud."`
				if detailed {
					want += `,"details":{"existing_registry_id":"registry-123"}`
				}
				want += "}\n"
				if response.Code != 409 || response.Body.String() != want || response.Header().Get("Content-Language") != "ms" {
					t.Fatal("language lost", response.Code, response.Body, response.Header())
				}
			})
		}
	}
}

func TestDetailedErrorResponseByteBoundary(t *testing.T) {
	const wire = `{"status":409,"error_code":"registry_exists","message":"Registry already exists.","details":{"existing_registry_id":"registry-123"}}` + "\n"
	for _, reserve := range []int{0, 1} {
		limits := DefaultEndpointLimits()
		limits.Response.Bytes = len(wire) - reserve
		declaration := registryError()
		router, err := NewRouter(ConfigureEndpoint(errorEndpoint("registry.create", "/registry"), Errors(declaration), Limits(limits)).Handle(returnHTTPError(declaration.New(RegistryErrorDetails{ExistingRegistryID: "registry-123"}))))
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/registry", nil))
		want, status := wire, 409
		if reserve == 1 {
			want, status = `{"status":500,"error_code":"internal_error","message":"Internal server error"}`+"\n", 500
		}
		if response.Code != status || response.Body.String() != want || response.Body.Len() > limits.Response.Bytes {
			t.Fatalf("budget %d: %d %s", limits.Response.Bytes, response.Code, response.Body)
		}
	}
}

type ErrorOmissionValue string

func (ErrorOmissionValue) IsZero() bool { runtime.Goexit(); return false }

type ErrorOmissionDetails struct {
	Value ErrorOmissionValue `json:"value,omitzero"`
}

func TestDetailedErrorContainsInputInspectionGoexit(t *testing.T) {
	const root = "github.com/weiloon1234/Foundry-Go/http.ErrorOmissionDetails"
	descriptor := contract.DefineJSON[ErrorOmissionDetails](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "value", Type: "string"}}},
		{ID: "string", Kind: contract.StringKind},
	}})
	declaration := DefineErrorDetails("omission_failed", 409, "Omission failed.", descriptor)
	router, err := NewRouter(ConfigureEndpoint(errorEndpoint("omission", "/omission"), Errors(declaration)).Handle(returnHTTPError(declaration.New(ErrorOmissionDetails{Value: "private"}))))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(response, httptest.NewRequest("GET", "/omission", nil))
	}()
	<-done
	if response.Code != 500 || response.Body.String() != `{"status":500,"error_code":"internal_error","message":"Internal server error"}`+"\n" {
		t.Fatal("input inspection Goexit escaped", response.Code, response.Body)
	}
}
