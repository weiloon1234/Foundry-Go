package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
	stdhttp "net/http"
	"reflect"
	"strings"
	"time"
)

// Cookie is an immutable typed declaration. Models, enums and named scalar
// values retain their types through Read and Set. It is not a session or guard.
type Cookie[V any] struct {
	name    CookieName
	codec   CookieCodec[V]
	options CookieOptions
	owned   bool
	err     error
}

// DefineCookie captures scope and codec. Validate during assembly; operations
// also reject an invalid declaration. Custom codecs must be bounded and safe for
// concurrent use. Foundry waits for their completion and catches panic/Goexit.
func DefineCookie[V any](name CookieName, codec CookieCodec[V], options CookieOptions) Cookie[V] {
	owned, err := compileCookieOptions(name, options)
	if err == nil && nilCookieValue(codec) {
		err = fault.New(fault.Invalid, "cookie requires a codec")
	}
	return Cookie[V]{name: name, codec: codec, options: owned, owned: err == nil && cookieCodecOwned(codec), err: err}
}

// invoke runs codec work under the cheapest boundary containing its failures.
func (c Cookie[V]) invoke(operation string, fn func() error) error {
	if c.owned {
		return callback.Invoke(operation, fn)
	}
	return callback.Isolated(operation, fn)
}
func (c Cookie[V]) Name() CookieName       { return c.name }
func (c Cookie[V]) Options() CookieOptions { return c.options }
func (c Cookie[V]) String() string         { return "Cookie(" + string(c.name) + ")" }
func (c Cookie[V]) GoString() string       { return c.String() }
func (c Cookie[V]) Validate() error {
	if c.err != nil {
		return c.err
	}
	if nilCookieValue(c.codec) {
		return fault.New(fault.Invalid, "cookie requires a codec")
	}
	_, err := compileCookieOptions(c.name, c.options)
	return err
}

// Read distinguishes absent from present-empty. Unrelated malformed cookies are
// ignored; only this name's own malformed value is a BadRequest. Repeated
// occurrences of this name, including identical values, read as absent because
// browser path ordering is not identity. Prefer __Host- names for credentials.
func (c Cookie[V]) Read(r *stdhttp.Request) (value.Optional[V], error) {
	text, present, err := c.readText(r)
	if err != nil || !present {
		return value.Optional[V]{}, err
	}
	decoded, err := c.decode(r.Context(), text)
	if err != nil {
		return value.Optional[V]{}, err
	}
	return value.Set(decoded), nil
}

// Set validates and formats before appending one Set-Cookie field. Invoke before
// response headers are committed. It never replaces other cookies or wraps w.
func (c Cookie[V]) Set(ctx context.Context, w stdhttp.ResponseWriter, v V) error {
	text, err := c.format(ctx, v)
	if err != nil {
		return err
	}
	return c.publish(w, text, false)
}

// Clear expires exactly this declaration's name/domain/path, retaining security
// flags. Deleting one path cannot remove cookies deliberately set at other paths.
func (c Cookie[V]) Clear(w stdhttp.ResponseWriter) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return c.publish(w, "", true)
}
func (c Cookie[V]) readText(r *stdhttp.Request) (string, bool, error) {
	if err := c.Validate(); err != nil {
		return "", false, err
	}
	if r == nil {
		return "", false, fault.New(fault.Invalid, "cookie read requires a request")
	}
	if err := r.Context().Err(); err != nil {
		return "", false, err
	}
	return findRequestCookie(r.Header.Values("Cookie"), c.name)
}

// findRequestCookie scans Cookie fields pair by pair without allocating for
// unrelated cookies. Browsers send cookies owned by other applications and
// scripts (JSON, quotes, non-ASCII, nameless pairs, trailing separators); none of
// them can reject a request. Only this name's own wire value must satisfy the
// native cookie grammar. Repeated occurrences are indistinguishable scopes, e.g.
// a sibling domain's tossed cookie, so the value is treated as absent. Use a
// __Host- name to prevent other hosts from supplying a same-name cookie.
func findRequestCookie(lines []string, name CookieName) (string, bool, error) {
	if len(lines) > maxCookieRequestFields {
		return "", false, BadRequest
	}
	size := 0
	for _, line := range lines {
		if len(line) > maxCookieRequestBytes-size {
			return "", false, BadRequest
		}
		size += len(line)
	}
	var found string
	present, repeated := false, false
	pairs := 0
	for _, line := range lines {
		for rest := line; rest != ""; {
			var pair string
			pair, rest, _ = strings.Cut(rest, ";")
			pair = strings.Trim(pair, " \t")
			if pair == "" {
				continue
			}
			if pairs++; pairs > maxCookieRequestPairs {
				return "", false, BadRequest
			}
			key, text, hasValue := strings.Cut(pair, "=")
			if !hasValue || strings.Trim(key, " \t") != string(name) {
				continue
			}
			if present {
				repeated = true
				continue
			}
			value, err := nativeCookieValue(name, strings.Trim(text, " \t"))
			if err != nil {
				return "", false, err
			}
			found, present = value, true
		}
	}
	if repeated {
		return "", false, nil
	}
	return found, present, nil
}

// nativeCookieValue applies Go's cookie-value grammar, including optional
// surrounding quotes, to exactly one requested pair.
func nativeCookieValue(name CookieName, text string) (string, error) {
	if len(text) > MaxCookieBytes {
		return "", BadRequest
	}
	cookies, err := stdhttp.ParseCookie(string(name) + "=" + text)
	if err != nil || len(cookies) != 1 {
		return "", BadRequest.WithCause(err)
	}
	return cookies[0].Value, nil
}
func (c Cookie[V]) decode(ctx context.Context, text string) (V, error) {
	var result V
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var failed error
	err := c.invoke("cookie decoder", func() error { result, failed = c.codec.Parse(text); return nil })
	if err != nil {
		var zero V
		return zero, fault.Wrap(fault.Internal, "cookie decoder callback failed", err)
	}
	if failed != nil {
		var zero V
		return zero, BadRequest.WithCause(failed)
	}
	if err := ctx.Err(); err != nil {
		var zero V
		return zero, err
	}
	return result, nil
}
func (c Cookie[V]) format(ctx context.Context, v V) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if ctx == nil {
		return "", fault.New(fault.Invalid, "cookie write requires a context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var text string
	var failed error
	err := c.invoke("cookie encoder", func() error { text, failed = c.codec.Format(v); return nil })
	if err != nil {
		return "", fault.Wrap(fault.Internal, "cookie encoder callback failed", err)
	}
	if failed != nil {
		return "", fault.Wrap(fault.Invalid, "cookie value could not be encoded", failed)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(text) > MaxCookieBytes {
		return "", fault.New(fault.Invalid, "cookie value exceeds its byte bound")
	}
	return text, nil
}
func (c Cookie[V]) publish(w stdhttp.ResponseWriter, text string, remove bool) error {
	if nilCookieValue(w) {
		return fault.New(fault.Invalid, "cookie write requires a response writer")
	}
	header, err := c.header(text, remove)
	if err != nil {
		return err
	}
	w.Header().Add("Set-Cookie", header)
	return nil
}
func (c Cookie[V]) header(text string, remove bool) (string, error) {
	cookie := nativeCookie(c.name, c.options, text)
	if remove {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0).UTC()
	}
	if err := cookie.Valid(); err != nil {
		return "", fault.New(fault.Invalid, "invalid cookie wire value")
	}
	header := cookie.String()
	if len(header) > MaxCookieBytes {
		return "", fault.New(fault.Invalid, "cookie exceeds its complete header bound")
	}
	return header, nil
}

func nilCookieValue(v any) bool {
	if v == nil {
		return true
	}
	reflected := reflect.ValueOf(v)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}
