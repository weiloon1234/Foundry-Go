package http

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/value"
	stdhttp "net/http"
)

// SignedCookie preserves the cookie's concrete value type while authenticating
// an envelope before invoking its decoder. Wire values are URL-safe encoded.
// MaxAge or Expires is enforced in the signed envelope. A session cookie with
// neither has no server-side expiry; credential lifetime belongs to authentication.
type SignedCookie[V any] struct {
	cookie Cookie[V]
	signer CookieSigner
}

func (c SignedCookie[V]) String() string                       { return "SignedCookie(" + string(c.cookie.Name()) + ")" }
func (c SignedCookie[V]) GoString() string                     { return c.String() }
func (c Cookie[V]) Signed(signer CookieSigner) SignedCookie[V] { return SignedCookie[V]{c, signer} }
func (c SignedCookie[V]) Name() CookieName                     { return c.cookie.Name() }
func (c SignedCookie[V]) Options() CookieOptions               { return c.cookie.Options() }
func (c SignedCookie[V]) Validate() error {
	if err := c.cookie.Validate(); err != nil {
		return err
	}
	return c.signer.Validate()
}
func (c SignedCookie[V]) Read(r *stdhttp.Request) (value.Optional[V], error) {
	if err := c.Validate(); err != nil {
		return value.Optional[V]{}, err
	}
	text, present, err := c.cookie.readText(r)
	if err != nil || !present {
		return value.Optional[V]{}, err
	}
	decoded, err := c.signer.open(c.cookie.name, c.cookie.options, text)
	if err != nil {
		if errors.Is(err, ErrInvalidSignedCookie) {
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
func (c SignedCookie[V]) Set(ctx context.Context, w stdhttp.ResponseWriter, v V) error {
	if err := c.Validate(); err != nil {
		return err
	}
	text, err := c.cookie.format(ctx, v)
	if err != nil {
		return err
	}
	envelope, err := c.signer.seal(c.cookie.name, c.cookie.options, text)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.cookie.publish(w, envelope, false)
}
func (c SignedCookie[V]) Clear(w stdhttp.ResponseWriter) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return c.cookie.Clear(w)
}
