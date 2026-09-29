package http

import (
	"context"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type cookieEncryptionError string

func (e cookieEncryptionError) Error() string { return string(e) }

// ErrInvalidEncryptedCookie identifies malformed, tampered, expired, rescoped
// or retired-key encrypted cookies through errors.Is. Transport reads wrap it as
// a shared BadRequest.
const ErrInvalidEncryptedCookie cookieEncryptionError = "invalid encrypted cookie"

// encryptedCookiePurpose separates cookie ciphertexts from every other use of
// the same keyring; the cookie scope is the authenticated binding.
const encryptedCookiePurpose encryption.Purpose = "foundry.cookie.v1"

// CookieEncrypter supplies confidentiality, integrity, scope binding, expiry and
// key rotation through the application's encryption keyring (AES-256-GCM).
// New values use the keyring's active key; retained keys decrypt existing
// values, so rotation keeps sessions readable until an old key is removed. It
// does not revoke values or prevent replay before expiry.
type CookieEncrypter struct {
	keyring *encryption.Keyring
	clock   clock.Clock
}

func (e CookieEncrypter) String() string   { return secret.Redacted }
func (e CookieEncrypter) GoString() string { return secret.Redacted }

func NewCookieEncrypter(keyring *encryption.Keyring, applicationClock clock.Clock) (CookieEncrypter, error) {
	encrypter := CookieEncrypter{keyring: keyring, clock: applicationClock}
	if err := encrypter.Validate(); err != nil {
		return CookieEncrypter{}, err
	}
	return encrypter, nil
}

func (e CookieEncrypter) Validate() error {
	if err := e.keyring.Validate(); err != nil {
		return err
	}
	if nilCookieValue(e.clock) {
		return fault.New(fault.Invalid, "cookie encrypter requires an application clock")
	}
	return nil
}

func encryptedCookieBinding(name CookieName, options CookieOptions) (encryption.Context, error) {
	return encryption.NewContext(encryptedCookiePurpose, secret.New(cookieScope(name, options)))
}

// seal encrypts "v1.<expires>.<text>"; expiry zero means a session cookie.
func (e CookieEncrypter) seal(ctx context.Context, name CookieName, options CookieOptions, text string) (string, error) {
	now, err := signingTime(e.clock, "cookie encryption clock")
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
		return "", fault.New(fault.Invalid, "encrypted cookie expiry must be in the future")
	}
	binding, err := encryptedCookieBinding(name, options)
	if err != nil {
		return "", err
	}
	ciphertext, err := e.keyring.Encrypt(ctx, binding, secret.New("v1."+strconv.FormatInt(expires, 10)+"."+text))
	if err != nil {
		return "", err
	}
	if len(ciphertext.Encoded()) > MaxCookieBytes {
		return "", fault.New(fault.Invalid, "encrypted cookie exceeds its envelope bound")
	}
	return ciphertext.Encoded(), nil
}

func (e CookieEncrypter) open(ctx context.Context, name CookieName, options CookieOptions, envelope string) (string, error) {
	if len(envelope) > MaxCookieBytes {
		return "", ErrInvalidEncryptedCookie
	}
	ciphertext, err := encryption.ParseCiphertext(envelope)
	if err != nil {
		return "", ErrInvalidEncryptedCookie
	}
	binding, err := encryptedCookieBinding(name, options)
	if err != nil {
		return "", err
	}
	plain, err := e.keyring.Decrypt(ctx, binding, ciphertext)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return "", canceled
		}
		return "", ErrInvalidEncryptedCookie
	}
	version, rest, ok := strings.Cut(plain.Reveal(), ".")
	expiresText, text, found := strings.Cut(rest, ".")
	if !ok || !found || version != "v1" {
		return "", ErrInvalidEncryptedCookie
	}
	expires, err := strconv.ParseInt(expiresText, 10, 64)
	if err != nil || expires < 0 || strconv.FormatInt(expires, 10) != expiresText {
		return "", ErrInvalidEncryptedCookie
	}
	if expires != 0 {
		now, err := signingTime(e.clock, "cookie encryption clock")
		if err != nil {
			return "", err
		}
		if now.Unix() >= expires {
			return "", ErrInvalidEncryptedCookie
		}
	}
	return text, nil
}
