package http

import (
	"bytes"
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func errorMessageCatalog(t testing.TB) *i18n.Catalog {
	t.Helper()
	locales, _ := i18n.NewLocaleSet("en", "en", "ms")
	definitions := append(validation.MessageDefinitions(), MessageDefinitions()...)
	definitions = append(definitions, i18n.MessageDefinition{Key: "fields.name"})
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, definitions, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {
		"fields.name": {Text: "Nama"}, "validation.min_length": {Forms: map[i18n.PluralForm]string{i18n.Other: "{{attribute}}: minimum {{min}} aksara."}},
		"validation.non_blank": {Text: "{{attribute}} diperlukan."}, "validation.file_max_size": {Forms: map[i18n.PluralForm]string{i18n.Other: "{{attribute}}: maksimum {{bytes}} bait."}},
		"http.error.validation_failed": {Text: "Pengesahan gagal"}, "http.error.bad_request": {Text: "Permintaan tidak sah"}, "http.input.type": {Text: "Jenis tidak sah."}, "http.input.value": {Text: "Nilai tidak sah."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
func TestRequestMessagesLocaleConcurrencyFallbackAndSafeErrors(t *testing.T) {
	name := validation.DefineField("name", func(v EndpointPatch) string { return v.Name }).WithLabel("Name").WithLabelKey("fields.name")
	term := validation.DefineField("q", func(v endpointParameters) value.Optional[string] { return v.Term }).WithLabel("Query")
	endpoint := patchEndpoint().WithBodyValidation(name.Rules(validation.MinLength[string](2))).WithQueryValidation(term.Rules(validation.Optional(validation.MinLength[string](2))))
	router, err := NewRouter(endpoint.Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		return EndpointReply{}, errors.New("private upstream password")
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, Locale(errorMessageCatalog(t)), Compression(DefaultCompressionConfig()), SecurityHeaders(DefaultSecurityHeadersConfig()))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			req := httptest.NewRequest("PATCH", "/items/"+endpointUserID+"?q=x", strings.NewReader(`{"name":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			locale := "en"
			if i%2 == 0 {
				locale = "ms"
			}
			req.Header.Set("Accept-Language", locale)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			failure := decodeFailure(t, response)
			if response.Code != 422 || len(failure.Issues) != 2 || failure.Issues[0].Path != "/body/name" || failure.Issues[1].Path != "/query/q" {
				t.Error("identity changed", failure)
				return
			}
			expected := "Name must contain at least 2 characters."
			if locale == "ms" {
				expected = "Nama: minimum 2 aksara."
			}
			if failure.Issues[0].Message != expected || response.Header().Get("Content-Language") != locale || !strings.Contains(response.Header().Get("Vary"), "Accept-Language") {
				t.Error("locale crossed requests", failure, response.Header())
			}
		})
	}
	wg.Wait()
	for _, tc := range []struct {
		path, body string
		status     int
	}{{"/items/" + endpointUserID, `{"name":7}`, 400}, {"/items/invalid", `{"name":"valid"}`, 400}, {"/items/" + endpointUserID, `{"name":"valid"}`, 500}} {
		req := httptest.NewRequest("PATCH", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "ms")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		failure := decodeFailure(t, response)
		if response.Code != tc.status || strings.Contains(response.Body.String(), "password") {
			t.Fatal(response.Code, response.Body)
		}
		if tc.status == 400 && (failure.Message != "Permintaan tidak sah" || len(failure.Issues) == 0 || failure.Issues[0].Message == "") {
			t.Fatal("decoder not localized", failure)
		}
		if tc.status == 500 && (len(failure.Issues) != 0 || failure.Message != "Internal server error") {
			t.Fatal("fault leaked", failure)
		}
	}
}
func TestFormAndUploadMessagesPreserveCleanup(t *testing.T) {
	catalog := errorMessageCatalog(t)
	name := validation.DefineField("name", func(v formInput) string { return v.Name }).WithLabelKey("fields.name")
	form, err := NewRouter(formEndpoint().WithBodyValidation(name.Rules(validation.NonBlank[string]())).Handle(func(context.Context, formRequest) (NoContent, error) { return NoContent{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(form, Locale(catalog))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		wire    string
		status  int
		message string
	}{{"name=", 422, "Nama diperlukan."}, {"name=ok&count=bad", 400, "Nilai tidak sah."}} {
		req := httptest.NewRequest("POST", "/form", strings.NewReader(tc.wire))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept-Language", "ms")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		failure := decodeFailure(t, response)
		if response.Code != tc.status || len(failure.Issues) != 1 || failure.Issues[0].Message != tc.message {
			t.Fatal(failure)
		}
	}
	directory := t.TempDir()
	file := validation.DefineField("primary", func(v multipartRequest) UploadedFile { return v.Primary }).WithLabel("Fail")
	upload, err := NewRouter(multipartEndpoint(directory).WithBodyValidation(file.Rules(validation.FileMaxSize[UploadedFile](1))).Handle(func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	handler, err = ApplyMiddleware(upload, Locale(catalog))
	if err != nil {
		t.Fatal(err)
	}
	wire, media := multipartWire(t, primaryUpload("large"))
	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire))
	req.Header.Set("Content-Type", media)
	req.Header.Set("Accept-Language", "ms")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	failure := decodeFailure(t, response)
	if response.Code != 422 || failure.Issues[0].Path != "/body/primary" || failure.Issues[0].Message != "Fail: maksimum 1 bait." {
		t.Fatal(failure)
	}
	assertMultipartCleanup(t, directory)
}

type messageUnwrapper struct {
	stdhttp.ResponseWriter
	action func()
}

func (w messageUnwrapper) Unwrap() stdhttp.ResponseWriter { w.action(); return w }
func TestMessagePresenterOwnsBadWrapperMethods(t *testing.T) {
	for _, action := range []func(){func() { panic("private") }, runtime.Goexit, func() {}} {
		recorder := httptest.NewRecorder()
		writer := messageUnwrapper{recorder, action}
		if _, err := findErrorPresenter(writer); err == nil {
			t.Fatal("bad unwrap accepted")
		}
	}
}
func TestLocaleRetainsNativeWriterCapabilitiesAndFindsBufferOwners(t *testing.T) {
	catalog := errorMessageCatalog(t)
	recorder := httptest.NewRecorder()
	owner := &localizedResponseWriter{ResponseWriter: recorder, presenter: errorPresenter{catalog: catalog, locale: "ms"}}
	writer := responseCapabilities(owner)
	if _, ok := writer.(stdhttp.Flusher); !ok {
		t.Fatal("flusher lost")
	}
	if _, ok := writer.(stdhttp.Hijacker); ok {
		t.Fatal("hijacker invented")
	}
	found, err := findErrorPresenter(writer)
	if err != nil || found != &owner.presenter {
		t.Fatal("presenter lost behind capabilities", err)
	}
}

func TestPathValidationLocalePreferenceAndStartupMismatch(t *testing.T) {
	field := validation.DefineField("user", func(v userPath) string { return v.User.String() }).WithLabel("User")
	router, err := NewRouter(patchEndpoint().WithPathValidation(field.Rules(validation.MinLength[string](100))).Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		t.Error("invalid path reached handler")
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	catalog := errorMessageCatalog(t)
	handler, err := ApplyMiddleware(router, Locale(catalog))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ header, preferred, message string }{{"ms", "en", "User must contain at least 100 characters."}, {"unsupported", "", "User must contain at least 100 characters."}, {"ms", "", "User: minimum 100 aksara."}} {
		req := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"ok"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", tc.header)
		if tc.preferred != "" {
			ctx, err := i18n.WithLocale(req.Context(), catalog, i18n.LocaleID(tc.preferred))
			if err != nil {
				t.Fatal(err)
			}
			req = req.WithContext(ctx)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		failure := decodeFailure(t, response)
		if response.Code != 422 || len(failure.Issues) != 1 || failure.Issues[0].Path != "/path/user" || failure.Issues[0].Message != tc.message {
			t.Fatal(failure)
		}
	}
	locales, _ := i18n.NewLocaleSet("en", "en")
	mismatch, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, []i18n.MessageDefinition{{Key: "validation.min_length"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMiddleware(router, Locale(mismatch)); err == nil {
		t.Fatal("registered signature mismatch passed startup")
	}
	// A manually opaque handler cannot expose its endpoint metadata. A runtime
	// mismatch must still retain the English rejection and status.
	opaque := stdhttp.HandlerFunc(router.ServeHTTP)
	fallback, err := ApplyMiddleware(opaque, Locale(mismatch))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"ok"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fallback.ServeHTTP(response, req)
	failure := decodeFailure(t, response)
	if response.Code != 422 || failure.Issues[0].Message != "User must contain at least 100 characters." {
		t.Fatal("presentation failure changed validation", failure)
	}
}
