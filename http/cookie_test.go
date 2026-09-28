package http

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/fault"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func cookieRequest(name CookieName, text string) *stdhttp.Request {
	r := httptest.NewRequest("GET", "https://example.test/", nil)
	r.AddCookie(&stdhttp.Cookie{Name: string(name), Value: text})
	return r
}
func TestCookiePresenceDuplicatesAndNativeQuoting(t *testing.T) {
	c := DefineCookie("preference", StringCookie[string](), DefaultCookieOptions())
	for _, input := range []struct {
		lines           []string
		want            string
		present, reject bool
	}{
		{nil, "", false, false},
		{[]string{"preference="}, "", true, false},
		{[]string{"another=1", "preference=\"a b,c\""}, "a b,c", true, false},
		{[]string{"preference=one; preference=one"}, "", false, true},
		{[]string{"preference=one", "preference=two"}, "", false, true},
		{[]string{"preference=\"unfinished"}, "", false, true},
		{[]string{"Preference=other"}, "", false, false},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header["Cookie"] = input.lines
		got, err := c.Read(r)
		if (err != nil) != input.reject {
			t.Fatalf("lines=%v error=%v", input.lines, err)
		}
		if input.reject {
			continue
		}
		v, present := got.Get()
		if v != input.want || present != input.present {
			t.Fatalf("read=%q,%v", v, present)
		}
	}
	r := httptest.NewRecorder()
	if err := c.Set(t.Context(), r, "a b,c"); err != nil {
		t.Fatal(err)
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "a b,c" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != stdhttp.SameSiteLaxMode {
		t.Fatalf("native cookie mismatch: %+v", cookies)
	}
}

func TestCookieScopeLifetimeAndExactRemoval(t *testing.T) {
	options := DefaultCookieOptions()
	options.Domain = ".EXAMPLE.test"
	options.Path = "/admin"
	options.MaxAge = time.Hour
	c := DefineCookie("preference", IntegerCookie[int16](), options)
	options.Path = "/changed"
	r := httptest.NewRecorder()
	r.Header().Add("Set-Cookie", "other=preserved")
	if err := c.Set(t.Context(), r, int16(42)); err != nil {
		t.Fatal(err)
	}
	if err := c.Clear(r); err != nil {
		t.Fatal(err)
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 3 || cookies[0].Name != "other" {
		t.Fatal("other cookies were replaced")
	}
	if cookies[1].Value != "42" || cookies[1].MaxAge != 3600 || cookies[1].Path != "/admin" || cookies[1].Domain != "example.test" {
		t.Fatalf("scope/lifetime mismatch: %+v", cookies[1])
	}
	removed := cookies[2]
	if removed.Name != "preference" || removed.Path != "/admin" || removed.Domain != "example.test" || removed.MaxAge != -1 || !removed.Expires.Before(time.Unix(2, 0)) || !removed.Secure || !removed.HttpOnly {
		t.Fatalf("removal scope mismatch: %+v", removed)
	}
}

func TestCookieRejectsInvalidPolicyValuesAndBoundsBeforeHeaders(t *testing.T) {
	valid := DefaultCookieOptions()
	invalid := []struct {
		name   CookieName
		change func(*CookieOptions)
	}{
		{"", func(*CookieOptions) {}},
		{"bad;name", func(*CookieOptions) {}},
		{"__Secure-id", func(o *CookieOptions) { o.Secure = false }},
		{"__Host-id", func(o *CookieOptions) { o.Domain = "example.test" }},
		{"__Host-id", func(o *CookieOptions) { o.Path = "/sub" }},
		{"id", func(o *CookieOptions) { o.Secure = false; o.SameSite = stdhttp.SameSiteNoneMode }},
		{"id", func(o *CookieOptions) { o.Secure = false; o.Partitioned = true }},
		{"id", func(o *CookieOptions) { o.Path = "relative" }},
		{"id", func(o *CookieOptions) { o.Path = "/x; HttpOnly" }},
		{"id", func(o *CookieOptions) { o.Domain = "example.test:443" }},
		{"id", func(o *CookieOptions) { o.Domain = "." }},
		{"id", func(o *CookieOptions) { o.MaxAge = -time.Second }},
		{"id", func(o *CookieOptions) { o.MaxAge = time.Millisecond }},
		{"id", func(o *CookieOptions) { o.SameSite = stdhttp.SameSite(99) }},
	}
	for _, test := range invalid {
		options := valid
		test.change(&options)
		c := DefineCookie(test.name, StringCookie[string](), options)
		r := httptest.NewRecorder()
		if c.Validate() == nil || c.Set(t.Context(), r, "value") == nil || len(r.Header()) != 0 {
			t.Errorf("invalid policy published %q", test.name)
		}
	}
	c := DefineCookie("value", StringCookie[string](), valid)
	for _, text := range []string{"bad\r\nSet-Cookie: x=y", "bad;value", strings.Repeat("x", MaxCookieBytes)} {
		r := httptest.NewRecorder()
		if c.Set(t.Context(), r, text) == nil || len(r.Header()) != 0 {
			t.Error("invalid/oversized value wrote headers")
		}
	}
	request := cookieRequest("value", "x")
	request.Header.Set("Cookie", strings.Repeat("x", maxCookieRequestBytes+1))
	if _, err := c.Read(request); !errors.Is(err, BadRequest) {
		t.Fatal("oversized cookie input accepted")
	}
	request = httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Cookie", strings.Repeat("x=y; ", maxCookieRequestPairs)+"value=x")
	if _, err := c.Read(request); !errors.Is(err, BadRequest) {
		t.Fatal("too many cookie pairs accepted")
	}
}

type cookieCallbacks struct {
	parse  func(string) (string, error)
	format func(string) (string, error)
}

func (c cookieCallbacks) Parse(s string) (string, error)  { return c.parse(s) }
func (c cookieCallbacks) Format(s string) (string, error) { return c.format(s) }

func TestCookieCodecsAreOwnedAndFailuresRemainSafe(t *testing.T) {
	for _, exit := range []bool{false, true} {
		fail := func(string) (string, error) {
			if exit {
				runtime.Goexit()
			}
			panic("private-cookie-value")
		}
		c := DefineCookie("value", cookieCallbacks{parse: fail, format: fail}, DefaultCookieOptions())
		w := httptest.NewRecorder()
		if err := c.Set(t.Context(), w, "input"); !errors.Is(err, fault.Internal) || strings.Contains(fmt.Sprint(err), "private-cookie-value") || len(w.Header()) != 0 {
			t.Fatalf("unsafe codec failure: %v", err)
		}
		if _, err := c.Read(cookieRequest("value", "input")); !errors.Is(err, fault.Internal) {
			t.Fatalf("decoder failure: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	c := DefineCookie("value", cookieCallbacks{format: func(s string) (string, error) { close(entered); <-release; return s, nil }, parse: func(s string) (string, error) { return s, nil }}, DefaultCookieOptions())
	w := httptest.NewRecorder()
	go func() { done <- c.Set(ctx, w, "input") }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("abandoned running codec")
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) || len(w.Header()) != 0 {
		t.Fatalf("cancellation published value: %v", err)
	}
	base64Cookie := DefineCookie("unicode", Base64Cookie(StringCookie[string]()), DefaultCookieOptions())
	w = httptest.NewRecorder()
	if err := base64Cookie.Set(t.Context(), w, "你好; world"); err != nil {
		t.Fatal(err)
	}
	value, err := base64Cookie.Read(cookieRequest(base64Cookie.Name(), w.Result().Cookies()[0].Value))
	decoded, _ := value.Get()
	if err != nil || decoded != "你好; world" {
		t.Fatalf("base64 round trip=%q %v", decoded, err)
	}
}

func TestCookieRejectsMissingAndTypedNilCodecs(t *testing.T) {
	for _, codec := range []CookieCodec[string]{nil, (*cookieCallbacks)(nil), Base64Cookie[string](nil)} {
		if DefineCookie("value", codec, DefaultCookieOptions()).Validate() == nil {
			t.Fatal("missing codec passed declaration validation")
		}
	}
}

func TestCookieConcurrentIndependentRequests(t *testing.T) {
	c := DefineCookie("count", IntegerCookie[int](), DefaultCookieOptions())
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			if err := c.Set(t.Context(), w, i); err != nil {
				t.Error(err)
				return
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Error("cookie missing")
				return
			}
			value, err := c.Read(cookieRequest(c.Name(), cookies[0].Value))
			v, _ := value.Get()
			if err != nil || v != i {
				t.Errorf("cross-request value: %d %v", v, err)
			}
		})
	}
	wg.Wait()
}

func FuzzCookieReadIsBounded(f *testing.F) {
	for _, text := range []string{"value=", "value=\"text\"", "value=a;value=b", "value=%00", "\r\n"} {
		f.Add(text)
	}
	c := DefineCookie("value", StringCookie[string](), DefaultCookieOptions())
	f.Fuzz(func(t *testing.T, text string) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Cookie", text)
		value, err := c.Read(r)
		if err != nil && value.IsSet() {
			t.Fatal("partial cookie result")
		}
		if v, ok := value.Get(); ok && len(v) > MaxCookieBytes {
			t.Fatal("oversized cookie result")
		}
	})
}
