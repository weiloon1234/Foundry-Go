package http

import (
	"context"
	"errors"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/value"
)

// EncryptedCookie preserves the cookie's concrete value type while keeping the
// value confidential. The encoded value is sealed with the application's
// encryption keyring, bound to this cookie's name, domain, path and security
// flags, and carries its MaxAge/Expires deadline inside the ciphertext. The
// decoder runs only after authentication succeeds. The envelope, including a
// 28-byte nonce/tag and base64url encoding, must fit MaxCookieBytes.
type EncryptedCookie[V any] struct {
	cookie    Cookie[V]
	encrypter CookieEncrypter
}

func (c EncryptedCookie[V]) String() string {
	return "EncryptedCookie(" + string(c.cookie.Name()) + ")"
}
func (c EncryptedCookie[V]) GoString() string { return c.String() }
func (c Cookie[V]) Encrypted(encrypter CookieEncrypter) EncryptedCookie[V] {
	return EncryptedCookie[V]{c, encrypter}
}
func (c EncryptedCookie[V]) Name() CookieName       { return c.cookie.Name() }
func (c EncryptedCookie[V]) Options() CookieOptions { return c.cookie.Options() }
func (c EncryptedCookie[V]) Validate() error {
	if err := c.cookie.Validate(); err != nil {
		return err
	}
	return c.encrypter.Validate()
}

// Read distinguishes absent from present values. Malformed, tampered, expired,
// rescoped and retired-key values return BadRequest wrapping
// ErrInvalidEncryptedCookie.
func (c EncryptedCookie[V]) Read(r *stdhttp.Request) (value.Optional[V], error) {
	if err := c.Validate(); err != nil {
		return value.Optional[V]{}, err
	}
	text, present, err := c.cookie.readText(r)
	if err != nil || !present {
		return value.Optional[V]{}, err
	}
	decoded, err := c.encrypter.open(r.Context(), c.cookie.name, c.cookie.options, text)
	if err != nil {
		if errors.Is(err, ErrInvalidEncryptedCookie) {
			return value.Optional[V]{}, BadRequest.WithCause(err)
		}
		return value.Optional[V]{}, err
	}
	parsed, err := c.cookie.decode(r.Context(), decoded)
	if err != nil {
		return value.Optional[V]{}, err
	}
	return value.Set(parsed), nil
}

func (c EncryptedCookie[V]) Set(ctx context.Context, w stdhttp.ResponseWriter, v V) error {
	if err := c.Validate(); err != nil {
		return err
	}
	text, err := c.cookie.format(ctx, v)
	if err != nil {
		return err
	}
	envelope, err := c.encrypter.seal(ctx, c.cookie.name, c.cookie.options, text)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.cookie.publish(w, envelope, false)
}

func (c EncryptedCookie[V]) Clear(w stdhttp.ResponseWriter) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return c.cookie.Clear(w)
}
