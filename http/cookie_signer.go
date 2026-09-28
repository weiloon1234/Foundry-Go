package http

import (
	"encoding/base64"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"strconv"
	"strings"
	"time"
)

type cookieSignatureError string

func (e cookieSignatureError) Error() string { return string(e) }

// ErrInvalidSignedCookie identifies malformed, tampered, expired or retired-key
// cookies through errors.Is. Transport reads wrap it as a shared BadRequest.
const ErrInvalidSignedCookie cookieSignatureError = "invalid signed cookie"

// CookieSigner supplies integrity, scope binding, expiry and rotation. It does
// not encrypt values, revoke sessions or prevent replay before expiry. Authentication
// remains a separate guard/provider concern. Application time is injected.
type CookieSigner struct {
	keys  SigningKeys
	clock clock.Clock
}

func (s CookieSigner) String() string   { return secret.Redacted }
func (s CookieSigner) GoString() string { return secret.Redacted }

func NewCookieSigner(keys SigningKeys, applicationClock clock.Clock) (CookieSigner, error) {
	if err := keys.Validate(); err != nil {
		return CookieSigner{}, err
	}
	if nilCookieValue(applicationClock) {
		return CookieSigner{}, fault.New(fault.Invalid, "cookie signer requires an application clock")
	}
	return CookieSigner{keys: keys, clock: applicationClock}, nil
}
func (s CookieSigner) Validate() error {
	if err := s.keys.Validate(); err != nil {
		return err
	}
	if nilCookieValue(s.clock) {
		return fault.New(fault.Invalid, "cookie signer requires an application clock")
	}
	return nil
}
func (s CookieSigner) now() (time.Time, error) {
	return signingTime(s.clock, "cookie signing clock")
}
func (s CookieSigner) seal(name CookieName, options CookieOptions, text string) (string, error) {
	now, err := s.now()
	if err != nil {
		return "", err
	}
	var expires int64
	switch {
	case options.MaxAge > 0:
		expires = now.Add(options.MaxAge).Unix()
	case !options.Expires.IsZero():
		expires = options.Expires.Unix()
	}
	if (options.MaxAge > 0 || !options.Expires.IsZero()) && expires <= now.Unix() {
		return "", fault.New(fault.Invalid, "signed cookie expiry must be in the future")
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(text))
	content := "v1." + string(s.keys.active) + "." + strconv.FormatInt(expires, 10) + "." + payload
	tag := s.keys.sign("foundry.cookie\x00"+cookieScope(name, options), content)
	if len(content)+1+len(tag) > MaxCookieBytes {
		return "", fault.New(fault.Invalid, "signed cookie exceeds its envelope bound")
	}
	return content + "." + tag, nil
}
func (s CookieSigner) open(name CookieName, options CookieOptions, envelope string) (string, error) {
	if len(envelope) > MaxCookieBytes {
		return "", ErrInvalidSignedCookie
	}
	parts := strings.Split(envelope, ".")
	if len(parts) != 5 || parts[0] != "v1" || !validSigningKeyID(SigningKeyID(parts[1])) {
		return "", ErrInvalidSignedCookie
	}
	expires, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || expires < 0 || strconv.FormatInt(expires, 10) != parts[2] {
		return "", ErrInvalidSignedCookie
	}
	content := envelope[:len(envelope)-len(parts[4])-1]
	if !s.keys.verify(SigningKeyID(parts[1]), "foundry.cookie\x00"+cookieScope(name, options), content, parts[4]) {
		return "", ErrInvalidSignedCookie
	}
	now, err := s.now()
	if err != nil {
		return "", err
	}
	if expires != 0 && now.Unix() >= expires {
		return "", ErrInvalidSignedCookie
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || base64.RawURLEncoding.EncodeToString(data) != parts[3] {
		return "", ErrInvalidSignedCookie
	}
	return string(data), nil
}
