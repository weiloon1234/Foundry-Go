package http

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const signedURLVersion = "v1"
const signedURLExpires = "expires"
const signedURLSignature = "signature"

// MaxSignedURLBytes bounds the complete absolute URL, including its envelope.
const MaxSignedURLBytes = 64 << 10

// MaxSignedURLQueryPairs includes the two framework-owned signing parameters.
const MaxSignedURLQueryPairs = 1024

type signedURLError string

func (e signedURLError) Error() string { return string(e) }

// ErrInvalidSignedURL identifies malformed, changed, expired or retired-key
// links through errors.Is. Registered signed routes return the shared 403.
const ErrInvalidSignedURL signedURLError = "invalid signed URL"

// URLSigner binds temporary links to their route ID, method, pattern, public
// origin and exact escaped path/query. It shares SigningKeys with cookies but
// uses a distinct HMAC purpose. Links are replayable until expiry; body content,
// authentication, one-time consumption and encryption remain separate concerns.
type URLSigner struct {
	keys   SigningKeys
	clock  clock.Clock
	policy signedURLPolicy
}

// signedURLPolicy is per signed route/endpoint. The zero policy accepts only
// expiring links whose every query parameter is authenticated.
type signedURLPolicy struct {
	permanent bool
	// ignored parameters are removed before verification; owned, never mutated.
	ignored map[string]bool
}

// signedURLForm selects the authenticated content: origin-relative links are
// not bound to an origin, and permanent links carry no expiry parameter.
type signedURLForm struct{ relative, permanent bool }

const signedURLRelativeVersion = "r1"

// maxSignedURLIgnoredParameters bounds declared unauthenticated parameters.
const maxSignedURLIgnoredParameters = 32

func (p signedURLPolicy) withIgnored(names []string) (signedURLPolicy, error) {
	if len(p.ignored)+len(names) > maxSignedURLIgnoredParameters {
		return p, fault.New(fault.Invalid, "signed URL ignored parameters exceed their bound")
	}
	owned := make(map[string]bool, len(p.ignored)+len(names))
	for name := range p.ignored {
		owned[name] = true
	}
	for _, name := range names {
		if !httpquery.ValidName(name) || name == signedURLExpires || name == signedURLSignature || owned[name] {
			return p, fault.New(fault.Invalid, "signed URL ignored parameters must be distinct valid names other than the signing parameters")
		}
		owned[name] = true
	}
	p.ignored = owned
	return p, nil
}

func (s URLSigner) String() string   { return secret.Redacted }
func (s URLSigner) GoString() string { return secret.Redacted }

func NewURLSigner(keys SigningKeys, applicationClock clock.Clock) (URLSigner, error) {
	s := URLSigner{keys: keys, clock: applicationClock}
	if err := s.Validate(); err != nil {
		return URLSigner{}, err
	}
	return s, nil
}
func (s URLSigner) Validate() error {
	if err := s.keys.Validate(); err != nil {
		return err
	}
	if nilCookieValue(s.clock) {
		return fault.New(fault.Invalid, "URL signer requires an application clock")
	}
	return nil
}

// signedURLPurpose separates route scopes and link forms. Absolute expiring
// links keep the original purpose, so existing links remain valid.
func signedURLPurpose(spec RouteSpec, pattern string, form signedURLForm) string {
	purpose := "foundry.url\x00" + string(spec.ID) + "\x00" + string(spec.Method) + "\x00" + pattern
	if form.relative {
		purpose += "\x00relative"
	}
	if form.permanent {
		purpose += "\x00permanent"
	}
	return purpose
}

func (s URLSigner) prepare(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "URL signing requires a context")
	}
	return ctx.Err()
}

func signedQuery(ctx context.Context, raw string) (url.Values, error) {
	return httpquery.Parse(ctx, raw, httpquery.Limits{Bytes: MaxSignedURLBytes, Pairs: MaxSignedURLQueryPairs})
}

func (s URLSigner) seal(ctx context.Context, spec RouteSpec, pattern string, origin Origin, relative string, expires time.Time) (string, error) {
	return s.sealForm(ctx, spec, pattern, origin, relative, expires, signedURLForm{})
}

// sealForm signs one link. Relative links ignore origin; permanent links ignore
// expires and require the route's explicit permanent-link opt-in.
func (s URLSigner) sealForm(ctx context.Context, spec RouteSpec, pattern string, origin Origin, relative string, expires time.Time, form signedURLForm) (string, error) {
	if err := s.prepare(ctx); err != nil {
		return "", err
	}
	if form.permanent && !s.policy.permanent {
		return "", fault.New(fault.Invalid, "permanent signed URLs require an explicit route opt-in")
	}
	base := relative
	if form.relative {
		if err := validateRelativeURL(relative); err != nil {
			return "", err
		}
	} else {
		absolute, err := AbsoluteURL(origin, relative)
		if err != nil {
			return "", err
		}
		base = absolute
	}
	if len(base) > MaxSignedURLBytes {
		return "", fault.New(fault.Invalid, "signed URL exceeds its byte bound")
	}
	_, raw, _ := strings.Cut(relative, "?")
	values, err := signedQuery(ctx, raw)
	if err != nil {
		return "", err
	}
	if _, ok := values[signedURLExpires]; ok {
		return "", fault.New(fault.Invalid, "signed URL reserves its expiry parameter")
	}
	if _, ok := values[signedURLSignature]; ok {
		return "", fault.New(fault.Invalid, "signed URL reserves its signature parameter")
	}
	for name := range values {
		if s.policy.ignored[name] {
			return "", fault.New(fault.Invalid, "signed URL query uses a parameter its route ignores during verification")
		}
	}
	// Generated queries never end with an empty slot. Reject rather than silently
	// changing the exact spelling that will be authenticated.
	if strings.HasSuffix(relative, "?") || strings.HasSuffix(raw, "&") {
		return "", fault.New(fault.Invalid, "signed URL has an empty trailing query slot")
	}
	separator := "?"
	if raw != "" {
		separator = "&"
	}
	content := base
	if !form.permanent {
		now, err := signingTime(s.clock, "URL signing clock")
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		expires = expires.UTC()
		if expires.Year() < 1970 || expires.Year() > 9999 || expires.Unix() <= now.Unix() {
			return "", fault.New(fault.Invalid, "signed URL expiry must be a future second within the supported range")
		}
		content += separator + signedURLExpires + "=" + strconv.FormatInt(expires.Unix(), 10)
		separator = "&"
	}
	version := signedURLVersion
	if form.relative {
		version = signedURLRelativeVersion
	}
	envelope := version + "." + string(s.keys.active) + "." + s.keys.sign(signedURLPurpose(spec, pattern, form), content)
	result := content + separator + signedURLSignature + "=" + envelope
	if len(result) > MaxSignedURLBytes {
		return "", fault.New(fault.Invalid, "signed URL exceeds its byte bound")
	}
	// Include reserved slots in the same bound used by verification.
	_, signedRaw, _ := strings.Cut(result, "?")
	if _, err := signedQuery(ctx, signedRaw); err != nil {
		return "", err
	}
	return result, nil
}

// open returns only the original application query. It authenticates exact bytes
// before consulting the clock; no domain codecs run at this boundary. Declared
// ignored parameters are removed first and never reach the application query.
func (s URLSigner) open(ctx context.Context, spec RouteSpec, pattern string, origin Origin, relative string) (string, error) {
	if err := s.prepare(ctx); err != nil {
		return "", err
	}
	if len(relative) > MaxSignedURLBytes {
		return "", ErrInvalidSignedURL
	}
	path, raw, found := strings.Cut(relative, "?")
	if !found {
		return "", ErrInvalidSignedURL
	}
	if len(s.policy.ignored) != 0 {
		raw = withoutIgnoredParameters(raw, s.policy.ignored)
		relative = path + "?" + raw
	}
	absolute, err := AbsoluteURL(origin, relative)
	if err != nil || len(absolute) > MaxSignedURLBytes {
		return "", ErrInvalidSignedURL
	}
	values, err := signedQuery(ctx, raw)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return "", canceled
		}
		return "", ErrInvalidSignedURL
	}
	expiring := len(values[signedURLExpires]) == 1
	if len(values[signedURLSignature]) != 1 || len(values[signedURLExpires]) > 1 || !expiring && !s.policy.permanent {
		return "", ErrInvalidSignedURL
	}
	signature := values[signedURLSignature][0]
	envelope := strings.Split(signature, ".")
	if len(envelope) != 3 || envelope[0] != signedURLVersion && envelope[0] != signedURLRelativeVersion || !validSigningKeyID(SigningKeyID(envelope[1])) {
		return "", ErrInvalidSignedURL
	}
	form := signedURLForm{relative: envelope[0] == signedURLRelativeVersion, permanent: !expiring}
	var expires int64
	tail := signedURLSignature + "=" + signature
	if expiring {
		expiresText := values[signedURLExpires][0]
		expires, err = strconv.ParseInt(expiresText, 10, 64)
		if err != nil || expires <= 0 || strconv.FormatInt(expires, 10) != expiresText || time.Unix(expires, 0).UTC().Year() > 9999 {
			return "", ErrInvalidSignedURL
		}
		tail = signedURLExpires + "=" + expiresText + "&" + tail
	}
	// Literal, canonical reserved names/values must be the last parameters.
	// This rejects encoded aliases, duplicates and unsigned trailing data.
	var applicationQuery string
	switch {
	case raw == tail:
	case strings.HasSuffix(raw, "&"+tail):
		applicationQuery = strings.TrimSuffix(raw, "&"+tail)
		if applicationQuery == "" || strings.HasSuffix(applicationQuery, "&") {
			return "", ErrInvalidSignedURL
		}
	default:
		return "", ErrInvalidSignedURL
	}
	base := absolute
	if form.relative {
		base = relative
	}
	// The signature is the last parameter; strip it and its one separator.
	content := strings.TrimSuffix(base, signedURLSignature+"="+signature)
	content = content[:len(content)-1]
	if !s.keys.verify(SigningKeyID(envelope[1]), signedURLPurpose(spec, pattern, form), content, envelope[2]) {
		return "", ErrInvalidSignedURL
	}
	if !expiring {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return applicationQuery, nil
	}
	now, err := signingTime(s.clock, "URL signing clock")
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if now.Unix() >= expires {
		return "", ErrInvalidSignedURL
	}
	return applicationQuery, nil
}

// withoutIgnoredParameters removes declared unauthenticated pairs, such as
// analytics parameters appended by a mail client, preserving all other bytes.
// Names are compared after query unescaping so encoded aliases are removed too.
func withoutIgnoredParameters(raw string, ignored map[string]bool) string {
	kept := make([]string, 0, strings.Count(raw, "&")+1)
	for pair := range strings.SplitSeq(raw, "&") {
		name, _, _ := strings.Cut(pair, "=")
		if decoded, err := url.QueryUnescape(name); err == nil && ignored[decoded] {
			continue
		}
		kept = append(kept, pair)
	}
	return strings.Join(kept, "&")
}
