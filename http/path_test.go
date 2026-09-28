package http

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type routeUser struct{}
type userPath struct{ User model.ID[routeUser] }
type textPath struct{ Text string }

func textRoute(pattern string) Route[textPath] {
	return DefineRoute(RouteSpec{ID: "text.show", Method: GET, Access: Public}, DefinePath(pattern,
		Param("text", StringPath[string](), func(p *textPath) *string { return &p.Text }),
	))
}

func TestTypedPathModelIdentityRoundTrip(t *testing.T) {
	id, err := model.NewID[routeUser]()
	if err != nil {
		t.Fatal(err)
	}
	route := DefineRoute(RouteSpec{ID: "users.show", Method: GET, Access: Public}, DefinePath("/users/{user}",
		Param("user", ModelIDPath[routeUser](), func(p *userPath) *model.ID[routeUser] { return &p.User }),
	))
	url, err := route.URL(userPath{User: id})
	if err != nil {
		t.Fatal(err)
	}
	var received model.ID[routeUser]
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, path userPath) {
		received = path.User
		if r.Pattern != "GET /users/{user}" || r.PathValue("user") != id.String() {
			t.Error("native request pattern or path binding is missing")
		}
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", url, nil))
	if recorder.Code != 204 || received != id {
		t.Fatalf("identity did not round trip: status=%d id=%v", recorder.Code, received)
	}
	for _, invalid := range []string{"bad-id", "00000000-0000-0000-0000-000000000000"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users/"+invalid, nil))
		if failure := decodeFailure(t, recorder); failure.Code != BadRequest {
			t.Fatalf("invalid ID: %+v", failure)
		}
	}
	if _, err := route.URL(userPath{}); err == nil {
		t.Fatal("nil identity generated a URL")
	}
}

func TestPathEscapingRoundTripsExactlyOnce(t *testing.T) {
	route := textRoute("/literal space/{text}")
	for _, value := range []string{"hello world", "a/b", "a%2Fb", "a?b#c", "中文", "+:;@&=", "..value"} {
		t.Run(value, func(t *testing.T) {
			location, err := route.URL(textPath{Text: value})
			if err != nil {
				t.Fatal(err)
			}
			var got string
			router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p textPath) { got = p.Text; w.WriteHeader(204) }))
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
			if recorder.Code != 204 || got != value {
				t.Fatalf("%q became %q through %q (status %d)", value, got, location, recorder.Code)
			}
		})
	}
	for _, invalid := range []string{"", ".", "..", "/", "\x00", "\r\n", string([]byte{0xff})} {
		if _, err := route.URL(textPath{Text: invalid}); err == nil {
			t.Fatalf("invalid path value accepted: %q", invalid)
		}
	}
}

func TestCatchAllEscapingAndEmptyTail(t *testing.T) {
	route := textRoute("/assets/{text...}")
	for _, value := range []string{"", "images/a b.png", "a/%2F/b", "中文/a"} {
		url, err := route.URL(textPath{Text: value})
		if err != nil {
			t.Fatal(err)
		}
		var got string
		router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p textPath) { got = p.Text; w.WriteHeader(204) }))
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", url, nil))
		if recorder.Code != 204 || got != value {
			t.Fatalf("catch-all %q through %q: status %d, got %q", value, url, recorder.Code, got)
		}
	}
	for _, value := range []string{"a/../b", "/a", "a/", "a//b", "."} {
		if _, err := route.URL(textPath{Text: value}); err == nil {
			t.Fatalf("noncanonical tail accepted: %q", value)
		}
	}
}

type smallPathNumber int8
type largePathNumber uint64

type naturalMemberCode string
type naturalMemberPath struct{ Code naturalMemberCode }

func TestNamedStringPathPreservesNaturalKeyType(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "members.show", Method: GET, Access: Public}, DefinePath("/members/{code}",
		Param("code", StringPath[naturalMemberCode](), func(p *naturalMemberPath) *naturalMemberCode { return &p.Code }),
	))
	const code naturalMemberCode = "member/42"
	location, err := route.URL(naturalMemberPath{Code: code})
	if err != nil || location != "/members/member%2F42" {
		t.Fatalf("natural key URL: %q %v", location, err)
	}
	var received naturalMemberCode
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, path naturalMemberPath) {
		received = path.Code
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
	if recorder.Code != 204 || received != code {
		t.Fatalf("natural key round trip: status %d value %q", recorder.Code, received)
	}
}

func TestPathIntegerWidthAndCanonicalFormat(t *testing.T) {
	for _, value := range []smallPathNumber{-128, -1, 0, 127} {
		text, err := IntegerPath[smallPathNumber]().Format(value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := IntegerPath[smallPathNumber]().Parse(text)
		if err != nil || got != value {
			t.Fatalf("integer round trip: %v %v", got, err)
		}
	}
	for _, value := range []string{"128", "-129", "+1", "01", "-0", " 1", "1 ", "18446744073709551616"} {
		if _, err := IntegerPath[smallPathNumber]().Parse(value); err == nil {
			t.Fatalf("invalid integer accepted: %q", value)
		}
	}
	large := largePathNumber(^uint64(0))
	text, err := IntegerPath[largePathNumber]().Format(large)
	if err != nil {
		t.Fatal(err)
	}
	got, err := IntegerPath[largePathNumber]().Parse(text)
	if err != nil || got != large {
		t.Fatalf("uint64 maximum: %v %v", got, err)
	}
	if _, err := IntegerPath[uint8]().Parse("256"); err == nil {
		t.Fatal("uint8 overflow accepted")
	}
	if _, err := IntegerPath[uint8]().Parse("-1"); err == nil {
		t.Fatal("negative unsigned value accepted")
	}
}

func TestInvalidPathDeclarationsFailBeforeServing(t *testing.T) {
	for _, pattern := range []string{"", "relative", "//users", "/a//b", "/a/../b", "/{text}/{text}", "/{text...}/last", "/{1bad}", "/{text", "/x%2Fy", "/x?query", "/x#fragment", "/x\\y"} {
		if err := textRoute(pattern).Validate(); err == nil {
			t.Errorf("invalid pattern accepted: %q", pattern)
		}
	}
	for _, path := range []Path[textPath]{
		DefinePath[textPath]("/{text}"),
		DefinePath("/static", Param("text", StringPath[string](), func(p *textPath) *string { return &p.Text })),
		DefinePath("/{text}", Param("text", StringPath[string](), func(p *textPath) *string { return &p.Text }), Param("text", StringPath[string](), func(p *textPath) *string { return &p.Text })),
		DefinePath("/{text}", Param[textPath, string]("text", nil, func(p *textPath) *string { return &p.Text })),
		DefinePath("/{text}", Param[textPath, string]("text", StringPath[string](), nil)),
	} {
		if _, err := path.URL(textPath{}); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid binding: %v", err)
		}
	}
}

func TestPathDeclarationOwnsItsBindings(t *testing.T) {
	bindings := []PathParameter[textPath]{Param("text", StringPath[string](), func(p *textPath) *string { return &p.Text })}
	path := DefinePath("/{text}", bindings...)
	bindings[0] = PathParameter[textPath]{}
	if url, err := path.URL(textPath{Text: "kept"}); err != nil || url != "/kept" {
		t.Fatalf("declaration was mutated: %q %v", url, err)
	}
}

func TestBrokenPathSelectorIsPrivateServerFailure(t *testing.T) {
	for _, selector := range []func(*textPath) *string{
		func(*textPath) *string { return nil },
		func(*textPath) *string { panic("private-path-panic") },
	} {
		route := DefineRoute(RouteSpec{ID: "broken", Method: GET, Access: Public}, DefinePath("/{text}", Param("text", StringPath[string](), selector)))
		router, err := NewRouter(route.HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) { t.Error("broken decoder reached handler") }))
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/value", nil))
		if failure := decodeFailure(t, recorder); failure.Code != InternalError || strings.Contains(recorder.Body.String(), "private-path-panic") {
			t.Fatalf("unsafe decoder failure: %+v", failure)
		}
		if _, err := route.URL(textPath{}); err == nil || strings.Contains(err.Error(), "private-path-panic") {
			t.Fatalf("unsafe URL failure: %v", err)
		}
	}
}

func FuzzTypedPathRoundTrip(f *testing.F) {
	for _, value := range []string{"member-42", "a/b", "a%2Fb", "a?b#c", "中文", "", "/", "..", "a/../b", "\x00", "//example.test"} {
		f.Add(value, false)
		f.Add(value, true)
	}
	f.Fuzz(func(t *testing.T, value string, tail bool) {
		if len(value) > 4096 {
			t.Skip()
		}
		pattern := "/prefix/{text}"
		if tail {
			pattern = "/prefix/{text...}"
		}
		route := textRoute(pattern)
		location, err := route.URL(textPath{Text: value})
		if err != nil {
			return
		}
		var decoded string
		called := false
		router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p textPath) {
			decoded = p.Text
			called = true
			w.WriteHeader(204)
		}))
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
		if !called || recorder.Code != 204 || decoded != value {
			t.Fatalf("path did not round trip: input %q URL %q status %d decoded %q", value, location, recorder.Code, decoded)
		}
	})
}
