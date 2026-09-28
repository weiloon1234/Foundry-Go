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
	keys  SigningKeys
	clock clock.Clock
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

func signedURLPurpose(spec RouteSpec, pattern string) string {
	return "foundry.url\x00" + string(spec.ID) + "\x00" + string(spec.Method) + "\x00" + pattern
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
	if err := s.prepare(ctx); err != nil {
		return "", err
	}
	absolute, err := AbsoluteURL(origin, relative)
	if err != nil {
		return "", err
	}
	if len(absolute) > MaxSignedURLBytes {
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
	// Generated queries never end with an empty slot. Reject rather than silently
	// changing the exact spelling that will be authenticated.
	if strings.HasSuffix(relative, "?") || strings.HasSuffix(raw, "&") {
		return "", fault.New(fault.Invalid, "signed URL has an empty trailing query slot")
	}
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
	separator := "?"
	if raw != "" {
		separator = "&"
	}
	content := absolute + separator + signedURLExpires + "=" + strconv.FormatInt(expires.Unix(), 10)
	envelope := signedURLVersion + "." + string(s.keys.active) + "." + s.keys.sign(signedURLPurpose(spec, pattern), content)
	result := content + "&" + signedURLSignature + "=" + envelope
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
// before consulting the clock; no domain codecs run at this boundary.
func (s URLSigner) open(ctx context.Context, spec RouteSpec, pattern string, origin Origin, relative string) (string, error) {
	if err := s.prepare(ctx); err != nil {
		return "", err
	}
	absolute, err := AbsoluteURL(origin, relative)
	if err != nil || len(absolute) > MaxSignedURLBytes {
		return "", ErrInvalidSignedURL
	}
	_, raw, found := strings.Cut(relative, "?")
	if !found {
		return "", ErrInvalidSignedURL
	}
	values, err := signedQuery(ctx, raw)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return "", canceled
		}
		return "", ErrInvalidSignedURL
	}
	if len(values[signedURLExpires]) != 1 || len(values[signedURLSignature]) != 1 {
		return "", ErrInvalidSignedURL
	}
	signature := values[signedURLSignature][0]
	envelope := strings.Split(signature, ".")
	if len(envelope) != 3 || envelope[0] != signedURLVersion || !validSigningKeyID(SigningKeyID(envelope[1])) {
		return "", ErrInvalidSignedURL
	}
	expiresText := values[signedURLExpires][0]
	expires, err := strconv.ParseInt(expiresText, 10, 64)
	if err != nil || expires <= 0 || strconv.FormatInt(expires, 10) != expiresText || time.Unix(expires, 0).UTC().Year() > 9999 {
		return "", ErrInvalidSignedURL
	}
	// Literal, canonical reserved names/values must be the last two parameters.
	// This rejects encoded aliases, duplicates and unsigned trailing data.
	tail := signedURLExpires + "=" + expiresText + "&" + signedURLSignature + "=" + signature
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
	content := strings.TrimSuffix(absolute, "&"+signedURLSignature+"="+signature)
	if !s.keys.verify(SigningKeyID(envelope[1]), signedURLPurpose(spec, pattern), content, envelope[2]) {
		return "", ErrInvalidSignedURL
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
