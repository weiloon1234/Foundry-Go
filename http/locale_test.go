package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestLocaleNegotiationPrecedenceAndValidation(t *testing.T) {
	catalog := errorMessageCatalog(t)
	failed := errors.New("preference store unavailable")
	negotiation := LocaleNegotiation{QueryParameter: "lang", Cookie: "locale", Preferred: func(r *stdhttp.Request) (i18n.LocaleID, bool, error) {
		switch r.Header.Get("X-Test-User") {
		case "malay":
			return "ms", true, nil
		case "broken":
			return "", false, failed
		}
		return "", false, nil
	}}
	var selected i18n.LocaleID
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		selected, _ = i18n.RequestLocale(r.Context())
		w.WriteHeader(204)
	}), LocaleWith(catalog, negotiation))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		target, cookie, user, accept string
		want                         i18n.LocaleID
	}{
		{"/", "", "", "ms", "ms"},
		{"/?lang=ms", "", "", "en", "ms"},
		{"/?lang=ms-MY", "", "", "en", "ms"},
		{"/?lang=fr", "locale=ms", "", "en", "ms"},
		{"/?lang=%zz&lang=ms", "", "", "en", "en"},
		{"/?lang=not_a_locale!", "", "malay", "en", "ms"},
		{"/", "locale=ms; other={\"json\":1}", "", "en", "ms"},
		{"/", "locale=en", "malay", "ms", "en"},
		{"/", "", "malay", "en", "ms"},
		{"/", "", "", "fr, *;q=0.1", "en"},
	} {
		request := httptest.NewRequest("GET", test.target, nil)
		if test.cookie != "" {
			request.Header.Set("Cookie", test.cookie)
		}
		if test.user != "" {
			request.Header.Set("X-Test-User", test.user)
		}
		request.Header.Set("Accept-Language", test.accept)
		response := httptest.NewRecorder()
		selected = ""
		handler.ServeHTTP(response, request)
		if response.Code != 204 || selected != test.want {
			t.Errorf("%s cookie=%q user=%q accept=%q: %d %q", test.target, test.cookie, test.user, test.accept, response.Code, selected)
		}
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("X-Test-User", "broken")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 500 {
		t.Fatalf("preference failure selected a default: %d", response.Code)
	}
	for _, invalid := range []LocaleNegotiation{{QueryParameter: "bad name"}, {Cookie: "bad;cookie"}} {
		if invalid.Validate() == nil {
			t.Errorf("invalid negotiation accepted: %+v", invalid)
		}
		if _, err := ApplyMiddleware(stdhttp.NotFoundHandler(), LocaleWith(catalog, invalid)); err == nil {
			t.Errorf("invalid negotiation assembled: %+v", invalid)
		}
	}
	if Locale(catalog).ID() != LocaleMiddlewareID || LocaleWith(catalog, negotiation).ID() != LocaleMiddlewareID {
		t.Fatal("locale middleware identity changed")
	}
}

// The locale selector is request metadata: typed endpoints and signed links
// ignore it, while an endpoint that declares the same name keeps its value.
func TestLocaleSelectorIsStrippedBeforeTypedAndSignedDecoding(t *testing.T) {
	now := testkit.NewClock(urlTestTime)
	signed := DefineEndpoint(DefineRoute(RouteSpec{ID: "reports.signed", Method: GET, Access: Public}, StaticPath("/signed")), EmptyQuery(), EmptyBody(), EmptyResponse(204)).Signed(urlTestSigner(t, now))
	type langQuery struct{ Lang value.Optional[string] }
	declared := DefineEndpoint(DefineRoute(RouteSpec{ID: "reports.declared", Method: GET, Access: Public}, StaticPath("/declared")),
		DefineQuery(OptionalQueryParam("lang", StringQuery[string](), func(q *langQuery) *value.Optional[string] { return &q.Lang })), EmptyBody(), EmptyResponse(204))
	var declaredValue string
	router, err := NewRouter(
		DefineEndpoint(DefineRoute(RouteSpec{ID: "reports.plain", Method: GET, Access: Public}, StaticPath("/plain")), EmptyQuery(), EmptyBody(), EmptyResponse(204)).Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
			return NoContent{}, nil
		}),
		declared.Handle(func(_ context.Context, in Input[NoPath, langQuery, NoBody]) (NoContent, error) {
			declaredValue, _ = in.Query.Lang.Get()
			return NoContent{}, nil
		}),
		signed.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) { return NoContent{}, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, PublicURLs(PublicURLConfig{AllowedOrigins: []Origin{urlTestOrigin}}), LocaleWith(errorMessageCatalog(t), LocaleNegotiation{QueryParameter: "lang"}))
	if err != nil {
		t.Fatal(err)
	}
	link, err := signed.URL(t.Context(), urlTestOrigin, NoPath{}, NoQuery{}, urlTestTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	origin := string(urlTestOrigin)
	for _, target := range []string{origin + "/plain?lang=ms", origin + "/declared?lang=ms", link + "&lang=ms", link} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
		if response.Code != 204 {
			t.Fatalf("%s: %d %s", target, response.Code, response.Body.String())
		}
	}
	if declaredValue != "ms" {
		t.Fatal("a declared parameter lost its value", declaredValue)
	}
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest("GET", origin+"/plain?other=1", nil))
	if unknown.Code != 400 {
		t.Fatal("undeclared parameters are still rejected", unknown.Code)
	}
}
