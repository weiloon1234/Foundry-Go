package http

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

var urlTestTime = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

const urlTestOrigin Origin = "https://app.example.test"

func urlTestSigner(t *testing.T, applicationClock clock.Clock) URLSigner {
	t.Helper()
	signer, err := NewURLSigner(cookieTestKeys(t, "url-key"), applicationClock)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
func urlTestSpec() RouteSpec { return RouteSpec{ID: "download.show", Method: GET, Access: Public} }

func TestSignedURLExactBytesScopeAndExpiry(t *testing.T) {
	now := testkit.NewClock(urlTestTime)
	signer := urlTestSigner(t, now)
	spec := urlTestSpec()
	const path = "/downloads/a%2Fb"
	const query = "q=a+b%2Bc&tag=one&tag=two"
	location, err := signer.seal(t.Context(), spec, "/downloads/{name}", urlTestOrigin, path+"?"+query, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	relative := strings.TrimPrefix(location, string(urlTestOrigin))
	got, err := signer.open(t.Context(), spec, "/downloads/{name}", urlTestOrigin, relative)
	if err != nil || got != query {
		t.Fatalf("round trip failed: %q %v", got, err)
	}
	for _, changed := range []struct {
		spec    RouteSpec
		pattern string
		origin  Origin
	}{
		{RouteSpec{ID: "different.show", Method: GET, Access: Public}, "/downloads/{name}", urlTestOrigin},
		{RouteSpec{ID: spec.ID, Method: POST, Access: Public}, "/downloads/{name}", urlTestOrigin},
		{spec, "/downloads/{other}", urlTestOrigin},
		{spec, "/downloads/{name}", "http://app.example.test"},
		{spec, "/downloads/{name}", "https://alias.example.test"},
		{spec, "/downloads/{name}", "https://app.example.test:8443"},
	} {
		if _, err := signer.open(t.Context(), changed.spec, changed.pattern, changed.origin, relative); !errors.Is(err, ErrInvalidSignedURL) {
			t.Fatalf("changed scope accepted: %+v %v", changed, err)
		}
	}
	if _, err := signer.open(t.Context(), spec, "/downloads/{name}", "HTTPS://APP.EXAMPLE.TEST:443", relative); err != nil {
		t.Fatalf("shared origin normalization diverged: %v", err)
	}
	now.Advance(time.Minute)
	if _, err := signer.open(t.Context(), spec, "/downloads/{name}", urlTestOrigin, relative); !errors.Is(err, ErrInvalidSignedURL) {
		t.Fatal("exact expiry boundary accepted")
	}
}

func TestSignedURLRejectsAmbiguousOrTamperedWire(t *testing.T) {
	signer := urlTestSigner(t, testkit.NewClock(urlTestTime))
	spec := urlTestSpec()
	location, err := signer.seal(t.Context(), spec, "/downloads/{name}", urlTestOrigin, "/downloads/a%2Fb?q=a+b%2Bc&tag=one&tag=two", urlTestTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	relative := strings.TrimPrefix(location, string(urlTestOrigin))
	for _, bad := range []string{
		relative + "&more=1", relative + "&", relative + "&signature=another",
		relative + "&expires=9999999999",
		strings.Replace(relative, "expires=", "%65xpires=", 1),
		strings.Replace(relative, "signature=", "%73ignature=", 1),
		strings.Replace(relative, "expires=", "expires=0", 1),
		strings.Replace(relative, "expires=", "expires=+", 1),
		strings.Replace(relative, "signature=v1.", "signature=v2.", 1),
		strings.Replace(relative, "signature=v1.url-key.", "signature=v1.unknown.", 1),
		strings.Replace(relative, "a%2Fb", "a%2fb", 1),
		strings.Replace(relative, "a%2Fb", "other", 1),
		strings.Replace(relative, "q=a+b", "q=a%20b", 1),
		strings.Replace(relative, "tag=one&tag=two", "tag=two&tag=one", 1),
		strings.Replace(relative, "?q=", "?expires=1&q=", 1),
		strings.Replace(relative, "?q=", "?%73ignature=bad&q=", 1),
		strings.Replace(relative, "?q=", "?q=%FF&z=", 1),
		strings.Replace(relative, "?q=", "?q=%XX&z=", 1),
		strings.Replace(relative, "?q=", "?q=bad;value&z=", 1),
		relative[:len(relative)-1], relative + "=", "/downloads/a", "/downloads/a?",
		"/downloads/a?expires=9223372036854775807&signature=v1.url-key.bad",
		relative + "#fragment",
	} {
		if got, err := signer.open(t.Context(), spec, "/downloads/{name}", urlTestOrigin, bad); got != "" || !errors.Is(err, ErrInvalidSignedURL) {
			t.Fatalf("invalid wire returned %q, %v for %q", got, err, bad)
		}
	}
}

func TestSignedURLRotationAndPurposeIsolation(t *testing.T) {
	now := testkit.NewClock(urlTestTime)
	old := SigningKey{ID: "old", Secret: secret.New(strings.Repeat("o", 32))}
	keys, err := NewSigningKeys(old)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewURLSigner(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	spec := urlTestSpec()
	location, err := signer.seal(t.Context(), spec, "/file", urlTestOrigin, "/file", now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	relative := strings.TrimPrefix(location, string(urlTestOrigin))
	rotated, err := NewURLSigner(cookieTestKeys(t, "new", old), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.open(t.Context(), spec, "/file", urlTestOrigin, relative); err != nil {
		t.Fatal(err)
	}
	newLocation, err := rotated.seal(t.Context(), spec, "/file", urlTestOrigin, "/file", now.Now().Add(time.Minute))
	if err != nil || !strings.Contains(newLocation, "signature=v1.new.") {
		t.Fatal("new key was not used")
	}
	removed, _ := NewURLSigner(cookieTestKeys(t, "new"), now)
	if _, err := removed.open(t.Context(), spec, "/file", urlTestOrigin, relative); !errors.Is(err, ErrInvalidSignedURL) {
		t.Fatal("removed key accepted")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if got := fmt.Sprintf(format, signer); got != secret.Redacted {
			t.Fatal("signer formatting exposed material")
		}
	}
	// Even identical key material cannot turn a cookie-purpose MAC into a URL MAC.
	content, signature, _ := strings.Cut(location, "&signature=")
	forged := strings.TrimPrefix(content, string(urlTestOrigin)) + "&signature=v1.old." + keys.sign("foundry.cookie\x00preference", content)
	if forged == relative || signature == "" {
		t.Fatal("invalid isolation test")
	}
	if _, err := signer.open(t.Context(), spec, "/file", urlTestOrigin, forged); !errors.Is(err, ErrInvalidSignedURL) {
		t.Fatal("cross-purpose MAC accepted")
	}
}

type urlClockFunc func() time.Time

func (f urlClockFunc) Now() time.Time { return f() }

func TestSignedURLClockCancellationAndBounds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	signer := urlTestSigner(t, testkit.NewClock(urlTestTime))
	spec := urlTestSpec()
	for _, c := range []context.Context{ctx, nil} {
		if got, err := signer.seal(c, spec, "/file", urlTestOrigin, "/file", urlTestTime.Add(time.Hour)); got != "" || err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	for _, expires := range []time.Time{{}, urlTestTime, urlTestTime.Add(500 * time.Millisecond), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if got, err := signer.seal(t.Context(), spec, "/file", urlTestOrigin, "/file", expires); got != "" || err == nil {
			t.Fatal("invalid expiry accepted")
		}
	}
	for _, path := range []string{
		"/file?expires=1", "/file?%65xpires=1", "/file?signature=x", "/file?%73ignature=x",
		"/file?", "/file?q=x&", "//foreign/file", "https://foreign/file",
		"/" + strings.Repeat("x", MaxSignedURLBytes), "/file?" + strings.Repeat("q=x&", MaxSignedURLQueryPairs-1) + "q=x",
	} {
		if got, err := signer.seal(t.Context(), spec, "/file", urlTestOrigin, path, urlTestTime.Add(time.Minute)); got != "" || err == nil {
			t.Fatal("invalid output accepted")
		}
	}
	for _, badClock := range []urlClockFunc{
		func() time.Time { panic("private clock failure") },
		func() time.Time { runtime.Goexit(); return time.Time{} },
		func() time.Time { return time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		bad := urlTestSigner(t, badClock)
		if got, err := bad.seal(t.Context(), spec, "/file", urlTestOrigin, "/file", urlTestTime.Add(time.Hour)); got != "" || err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("clock failure escaped")
		}
		// Invalid signatures are rejected without invoking a hostile clock.
		if _, err := bad.open(t.Context(), spec, "/file", urlTestOrigin, "/file?expires=9999999999&signature=v1.url-key.bad"); !errors.Is(err, ErrInvalidSignedURL) {
			t.Fatal("clock ran before MAC verification")
		}
	}
	var nilClock urlClockFunc
	if _, err := NewURLSigner(signer.keys, nilClock); !errors.Is(err, fault.Invalid) {
		t.Fatal("typed nil clock accepted")
	}
	if err := (URLSigner{}).Validate(); err == nil {
		t.Fatal("zero signer accepted")
	}

	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	owned := urlTestSigner(t, urlClockFunc(func() time.Time { close(started); <-release; return urlTestTime }))
	ctx, cancel = context.WithCancel(t.Context())
	go func() {
		_, err := owned.seal(ctx, spec, "/file", urlTestOrigin, "/file", urlTestTime.Add(time.Minute))
		done <- err
	}()
	<-started
	cancel()
	select {
	case <-done:
		t.Fatal("clock ownership ended before callback returned")
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestSignedURLConcurrentUse(t *testing.T) {
	signer := urlTestSigner(t, testkit.NewClock(urlTestTime))
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			location, err := signer.seal(t.Context(), urlTestSpec(), "/file", urlTestOrigin, "/file?q=a&q=b", urlTestTime.Add(time.Minute))
			if err != nil {
				t.Error(err)
				return
			}
			if got, err := signer.open(t.Context(), urlTestSpec(), "/file", urlTestOrigin, strings.TrimPrefix(location, string(urlTestOrigin))); got != "q=a&q=b" || err != nil {
				t.Error("concurrent round trip failed")
			}
		})
	}
	workers.Wait()
}

func FuzzSignedURLVerification(f *testing.F) {
	f.Add("/file?expires=1&signature=v1.key.tag")
	f.Add("/file?%65xpires=1&signature=x&more=1")
	f.Add("/file?q=%FF")
	keys, err := NewSigningKeys(SigningKey{ID: "fuzz", Secret: secret.New(strings.Repeat("f", 32))})
	if err != nil {
		f.Fatal(err)
	}
	signer, err := NewURLSigner(keys, testkit.NewClock(urlTestTime))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, relative string) {
		got, err := signer.open(t.Context(), urlTestSpec(), "/file", urlTestOrigin, relative)
		if err != nil && got != "" {
			t.Fatal("failure returned partial query")
		}
		if len(relative) > MaxSignedURLBytes && err == nil {
			t.Fatal("oversized URL accepted")
		}
	})
}
