package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"
)

// CookieName is case-sensitive and distinct from HTTP header names.
type CookieName string

func (n CookieName) Validate() error { return HeaderName(n).Validate() }

// MaxCookieBytes bounds a complete outgoing Set-Cookie field, including scope.
const MaxCookieBytes = 4096
const maxCookieRequestBytes = 16 << 10
const maxCookieRequestFields = 32
const maxCookieRequestPairs = 128

// CookieOptions owns scope and browser persistence. Zero MaxAge/Expires means a
// session cookie; Clear performs deletion explicitly. MaxAge takes precedence
// over Expires, matching browser semantics. Durations must be whole seconds.
// Native SameSite values are reused, not represented by framework strings.
type CookieOptions struct {
	Path        string
	Domain      string
	Secure      bool
	HTTPOnly    bool
	SameSite    stdhttp.SameSite
	Partitioned bool
	MaxAge      time.Duration
	Expires     time.Time
}

// DefaultCookieOptions returns host-only, Secure, HttpOnly, SameSite=Lax scope at /.
// Local plain-HTTP applications may explicitly set Secure=false. Do not infer it
// from an unvalidated forwarding header; request-dependent policy uses IsSecure.
func DefaultCookieOptions() CookieOptions {
	return CookieOptions{Path: "/", Secure: true, HTTPOnly: true, SameSite: stdhttp.SameSiteLaxMode}
}

func compileCookieOptions(name CookieName, o CookieOptions) (CookieOptions, error) {
	if err := name.Validate(); err != nil {
		return CookieOptions{}, err
	}
	if o.Path == "" || !strings.HasPrefix(o.Path, "/") || len(o.Path) > 1024 || len(o.Domain) > 253 {
		return CookieOptions{}, fault.New(fault.Invalid, "cookie requires a bounded absolute path and domain")
	}
	if o.MaxAge < 0 || o.MaxAge%time.Second != 0 || int64(int(o.MaxAge/time.Second)) != int64(o.MaxAge/time.Second) {
		return CookieOptions{}, fault.New(fault.Invalid, "cookie MaxAge requires nonnegative whole seconds")
	}
	if o.SameSite != 0 && o.SameSite != stdhttp.SameSiteDefaultMode && o.SameSite != stdhttp.SameSiteLaxMode && o.SameSite != stdhttp.SameSiteStrictMode && o.SameSite != stdhttp.SameSiteNoneMode {
		return CookieOptions{}, fault.New(fault.Invalid, "invalid cookie SameSite policy")
	}
	if (o.SameSite == stdhttp.SameSiteNoneMode || o.Partitioned) && !o.Secure {
		return CookieOptions{}, fault.New(fault.Invalid, "cross-site or partitioned cookies require Secure")
	}
	if o.Domain == "." {
		return CookieOptions{}, fault.New(fault.Invalid, "cookie domain cannot be a lone dot")
	}
	o.Domain = strings.ToLower(strings.TrimPrefix(o.Domain, "."))
	if !o.Expires.IsZero() {
		o.Expires = o.Expires.UTC()
		if o.Expires.Year() > 9999 {
			return CookieOptions{}, fault.New(fault.Invalid, "cookie expiry exceeds the HTTP date range")
		}
	}
	native := nativeCookie(name, o, "")
	if err := native.Valid(); err != nil {
		return CookieOptions{}, fault.New(fault.Invalid, "invalid cookie scope or lifetime")
	}
	if strings.HasPrefix(string(name), "__Secure-") && !o.Secure {
		return CookieOptions{}, fault.New(fault.Invalid, "__Secure- cookie requires Secure")
	}
	if strings.HasPrefix(string(name), "__Host-") && (!o.Secure || o.Domain != "" || o.Path != "/") {
		return CookieOptions{}, fault.New(fault.Invalid, "__Host- cookie requires Secure, host-only scope and Path=/")
	}
	if len(native.String()) > MaxCookieBytes {
		return CookieOptions{}, fault.New(fault.Invalid, "cookie scope exceeds its header bound")
	}
	return o, nil
}
func nativeCookie(name CookieName, o CookieOptions, text string) *stdhttp.Cookie {
	return &stdhttp.Cookie{Name: string(name), Value: text, Path: o.Path, Domain: o.Domain, Secure: o.Secure, HttpOnly: o.HTTPOnly, SameSite: o.SameSite, Partitioned: o.Partitioned, MaxAge: int(o.MaxAge / time.Second), Expires: o.Expires}
}

// cookieScope binds signatures to every scope/security attribute, not just name.
// NUL cannot occur in any validated attribute, and TTL is signed in the envelope.
func cookieScope(name CookieName, o CookieOptions) string {
	return string(name) + "\x00" + o.Domain + "\x00" + o.Path + "\x00" + strconv.FormatBool(o.Secure) + "\x00" + strconv.FormatBool(o.HTTPOnly) + "\x00" + strconv.Itoa(int(o.SameSite)) + "\x00" + strconv.FormatBool(o.Partitioned)
}
