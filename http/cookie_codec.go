package http

import (
	"encoding/base64"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/model"
)

// CookieCodec reuses the scalar conversion contract used by paths and queries.
// Cookie serialization has its own quoting, bounds and duplicate-name rules.
type CookieCodec[V any] = PathCodec[V]

// FloatCookie preserves finite native/named floats; use TextCookie for exact decimals.
func FloatCookie[V PathFloat]() CookieCodec[V] { return FloatPath[V]() }

func StringCookie[V ~string]() CookieCodec[V]                 { return StringPath[V]() }
func IntegerCookie[V PathInteger]() CookieCodec[V]            { return IntegerPath[V]() }
func BoolCookie[V ~bool]() CookieCodec[V]                     { return BoolPath[V]() }
func ModelIDCookie[M any]() CookieCodec[model.ID[M]]          { return ModelIDPath[M]() }
func TextCookie[V any, P PathTextPointer[V]]() CookieCodec[V] { return TextPath[V, P]() }

// Base64Cookie explicitly encodes arbitrary text into canonical URL-safe cookie
// octets. It does not sign or encrypt the value. Signed cookies already encode
// their payload, so they do not require this adapter.
func Base64Cookie[V any](codec CookieCodec[V]) CookieCodec[V] {
	if nilCookieValue(codec) {
		return nil
	}
	return base64CookieCodec[V]{codec}
}

type base64CookieCodec[V any] struct{ inner CookieCodec[V] }

func (c base64CookieCodec[V]) Parse(text string) (V, error) {
	var zero V
	if nilCookieValue(c.inner) || len(text) > MaxCookieBytes {
		return zero, fault.New(fault.Invalid, "invalid base64 cookie codec or value")
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || base64.RawURLEncoding.EncodeToString(data) != text {
		return zero, fault.New(fault.Invalid, "invalid base64 cookie representation")
	}
	return c.inner.Parse(string(data))
}
func (c base64CookieCodec[V]) Format(v V) (string, error) {
	if nilCookieValue(c.inner) {
		return "", fault.New(fault.Invalid, "cookie codec is missing")
	}
	text, err := c.inner.Format(v)
	if err != nil {
		return "", err
	}
	if len(text) > MaxCookieBytes || base64.RawURLEncoding.EncodedLen(len(text)) > MaxCookieBytes {
		return "", fault.New(fault.Invalid, "cookie value exceeds its byte bound")
	}
	return base64.RawURLEncoding.EncodeToString([]byte(text)), nil
}

// Built-in scalar and credential codecs run no application methods and never
// call runtime.Goexit, so cookie reads and writes use the in-goroutine panic
// boundary for them. Text, enum and custom codecs remain isolated.
func (stringPathCodec[V]) FoundryAdapter(frameworkadapter.Seal)    {}
func (integerPathCodec[V]) FoundryAdapter(frameworkadapter.Seal)   {}
func (floatPathCodec[V]) FoundryAdapter(frameworkadapter.Seal)     {}
func (boolPathCodec[V]) FoundryAdapter(frameworkadapter.Seal)      {}
func (modelIDPathCodec[M]) FoundryAdapter(frameworkadapter.Seal)   {}
func (credentialCookieCodec) FoundryAdapter(frameworkadapter.Seal) {}

// cookieCodecOwned reports whether every layer of a codec is framework-owned.
func cookieCodecOwned[V any](codec CookieCodec[V]) bool {
	switch c := codec.(type) {
	case describedURLCodec[V]:
		return c.err != nil || frameworkadapter.Is(c.codec)
	case base64CookieCodec[V]:
		return cookieCodecOwned(c.inner)
	}
	return frameworkadapter.Is(codec)
}
