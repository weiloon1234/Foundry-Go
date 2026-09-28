// Package webhook verifies bounded original webhook bytes before typed decoding.
// Verified identities are bound to a configured provider/account and are not a
// replacement for current domain authorization or durable idempotency.
package webhook

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Provider string
type DeliveryID string
type Protocol string

const (
	Standard Protocol = "standard_webhooks"
	Stripe   Protocol = "stripe"
)

var (
	InvalidSignature = errors.New("webhook signature or freshness is invalid")
	BodyTooLarge     = errors.New("webhook body exceeds its configured limit")
)

type Config struct {
	Provider     Provider
	Protocol     Protocol
	MaxBodyBytes int64
	Tolerance    time.Duration
}

func DefaultConfig(provider Provider, protocol Protocol) Config {
	return Config{Provider: provider, Protocol: protocol, MaxBodyBytes: 1 << 20, Tolerance: 5 * time.Minute}
}
func (c Config) Validate() error {
	if !identifier.Semantic(string(c.Provider)) || c.Protocol != Standard && c.Protocol != Stripe || c.MaxBodyBytes < 1 || c.MaxBodyBytes > 16<<20 || c.Tolerance < time.Second || c.Tolerance > time.Hour {
		return fault.New(fault.Invalid, "invalid webhook verification configuration")
	}
	return nil
}

type verificationKey struct {
	scheme   string
	material []byte
}

// Verifier retains immutable configuration and a bounded rotation keyring. Use
// separate endpoint secrets for each account; account comes from trusted setup,
// never from an unsigned header. Account values should be immutable domain IDs.
type Verifier[A any] struct {
	config  Config
	account A
	keys    []verificationKey
	clock   clock.Clock
}

// New supports Standard Webhooks whsec_ HMAC keys and whpk_ Ed25519 public keys,
// or Stripe endpoint secrets (used verbatim). No keys are loaded from requests.
func New[A any](config Config, account A, keys []secret.String, source clock.Clock) (*Verifier[A], error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if source == nil || len(keys) == 0 || len(keys) > 8 {
		return nil, fault.New(fault.Invalid, "webhook verifier requires a clock and bounded keyring")
	}
	v := &Verifier[A]{config: config, account: account, clock: source}
	for _, key := range keys {
		text := key.Reveal()
		if len(text) > 512 {
			return nil, fault.New(fault.Invalid, "invalid webhook verification key")
		}
		item := verificationKey{scheme: "v1"}
		if config.Protocol == Stripe {
			if !strings.HasPrefix(text, "whsec_") || len(text) < 22 {
				return nil, fault.New(fault.Invalid, "invalid Stripe endpoint key")
			}
			item.material = []byte(text)
		} else {
			prefix, encoded, ok := strings.Cut(text, "_")
			if !ok || prefix != "whsec" && prefix != "whpk" {
				return nil, fault.New(fault.Invalid, "invalid Standard Webhooks key")
			}
			material, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if err != nil {
				return nil, fault.New(fault.Invalid, "invalid Standard Webhooks key")
			}
			if prefix == "whpk" {
				if len(material) != ed25519.PublicKeySize {
					return nil, fault.New(fault.Invalid, "invalid webhook public key")
				}
				item.scheme = "v1a"
			} else if len(material) < 24 || len(material) > 64 {
				return nil, fault.New(fault.Invalid, "invalid webhook symmetric key")
			}
			item.material = material
		}
		v.keys = append(v.keys, item)
	}
	return v, nil
}
func (v *Verifier[A]) Validate() error {
	if v == nil || len(v.keys) == 0 || v.clock == nil {
		return fault.New(fault.Invalid, "webhook verifier is not configured")
	}
	return v.config.Validate()
}
func (Verifier[A]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("webhook verifier")) }

// Delivery contains only signature-authenticated metadata and the configured
// account. Its unexported proof prevents construction of a verified zero value.
type Delivery[A any] struct {
	verifier  *Verifier[A]
	id        DeliveryID
	timestamp time.Time
}

func (d Delivery[A]) ID() DeliveryID       { return d.id }
func (d Delivery[A]) Timestamp() time.Time { return d.timestamp }
func (d Delivery[A]) Provider() Provider {
	if d.verifier == nil {
		return ""
	}
	return d.verifier.config.Provider
}
func (d Delivery[A]) Account() A {
	if d.verifier == nil {
		return *new(A)
	}
	return d.verifier.account
}
func (Delivery[A]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("verified webhook delivery"))
}

type deliveryContextKey[A any] struct{ verifier *Verifier[A] }

func (v *Verifier[A]) WithDelivery(ctx context.Context, d Delivery[A]) (context.Context, error) {
	if ctx == nil || d.verifier != v || v == nil || d.id == "" {
		return ctx, fault.New(fault.Invalid, "webhook delivery belongs to another verifier")
	}
	return context.WithValue(ctx, deliveryContextKey[A]{v}, d), nil
}

// FromContext returns typed metadata attached by this exact verifier. It is not
// a credential and does not authorize future requests or application resources.
func (v *Verifier[A]) FromContext(ctx context.Context) (Delivery[A], bool) {
	if ctx == nil || v == nil {
		return Delivery[A]{}, false
	}
	d, ok := ctx.Value(deliveryContextKey[A]{v}).(Delivery[A])
	return d, ok
}

// VerifyRequest reads at most MaxBodyBytes+1 bytes, closes the body it reads and
// restores the exact signed bytes after successful verification for typed decoding.
// Place it before middleware that transforms/decompresses the body.
func (v *Verifier[A]) VerifyRequest(r *http.Request) (Delivery[A], error) {
	if err := v.Validate(); err != nil {
		return Delivery[A]{}, err
	}
	if r == nil || r.Body == nil || r.Method != http.MethodPost || r.Header.Get("Content-Encoding") != "" {
		return Delivery[A]{}, InvalidSignature
	}
	if r.ContentLength > v.config.MaxBodyBytes {
		return Delivery[A]{}, BodyTooLarge
	}
	original := r.Body
	data, err := io.ReadAll(io.LimitReader(original, v.config.MaxBodyBytes+1))
	closed := original.Close()
	if err != nil || closed != nil {
		return Delivery[A]{}, fault.New(fault.Invalid, "webhook body could not be read")
	}
	if int64(len(data)) > v.config.MaxBodyBytes {
		return Delivery[A]{}, BodyTooLarge
	}
	delivery, err := v.Verify(r.Context(), r.Header, data)
	if err != nil {
		return Delivery[A]{}, err
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	return delivery, nil
}

// Verify authenticates the original bytes. It intentionally permits a valid
// duplicate within the freshness window; use existing idempotency for effects.
func (v *Verifier[A]) Verify(ctx context.Context, headers http.Header, body []byte) (Delivery[A], error) {
	if err := v.Validate(); err != nil {
		return Delivery[A]{}, err
	}
	if ctx == nil {
		return Delivery[A]{}, InvalidSignature
	}
	if err := ctx.Err(); err != nil {
		return Delivery[A]{}, err
	}
	if int64(len(body)) > v.config.MaxBodyBytes {
		return Delivery[A]{}, BodyTooLarge
	}
	var id, stamp string
	var signatures []signature
	var err error
	if v.config.Protocol == Standard {
		id, err = singleHeader(headers, "Webhook-Id", 256)
		if err != nil || !deliveryID(id) {
			return Delivery[A]{}, InvalidSignature
		}
		stamp, err = singleHeader(headers, "Webhook-Timestamp", 20)
		if err != nil {
			return Delivery[A]{}, InvalidSignature
		}
		text, err := singleHeader(headers, "Webhook-Signature", 8192)
		if err != nil {
			return Delivery[A]{}, InvalidSignature
		}
		parts := strings.Fields(text)
		if len(parts) == 0 || len(parts) > 16 {
			return Delivery[A]{}, InvalidSignature
		}
		for _, part := range parts {
			scheme, encoded, ok := strings.Cut(part, ",")
			if !ok {
				return Delivery[A]{}, InvalidSignature
			}
			if scheme != "v1" && scheme != "v1a" {
				continue
			}
			data, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if err != nil {
				return Delivery[A]{}, InvalidSignature
			}
			signatures = append(signatures, signature{scheme, data})
		}
	} else {
		text, e := singleHeader(headers, "Stripe-Signature", 8192)
		if e != nil {
			return Delivery[A]{}, InvalidSignature
		}
		seenTimestamp := false
		parts := strings.Split(text, ",")
		if len(parts) > 32 {
			return Delivery[A]{}, InvalidSignature
		}
		for _, part := range parts {
			name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				return Delivery[A]{}, InvalidSignature
			}
			switch name {
			case "t":
				if seenTimestamp {
					return Delivery[A]{}, InvalidSignature
				}
				seenTimestamp = true
				stamp = value
			case "v1":
				data, e := hex.DecodeString(value)
				if e != nil {
					return Delivery[A]{}, InvalidSignature
				}
				signatures = append(signatures, signature{"v1", data})
			}
		}
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || seconds <= 0 || strconv.FormatInt(seconds, 10) != stamp {
		return Delivery[A]{}, InvalidSignature
	}
	timestamp := time.Unix(seconds, 0).UTC()
	var now time.Time
	if err := callback.Isolated("webhook clock", func() error { now = v.clock.Now().UTC(); return nil }); err != nil {
		return Delivery[A]{}, err
	}
	if now.IsZero() || timestamp.Before(now.Add(-v.config.Tolerance)) || timestamp.After(now.Add(v.config.Tolerance)) {
		return Delivery[A]{}, InvalidSignature
	}
	prefix := stamp + "."
	if v.config.Protocol == Standard {
		prefix = id + "." + prefix
	}
	matched := false
	message := make([]byte, 0, len(prefix)+len(body))
	message = append(message, prefix...)
	message = append(message, body...)
	for _, key := range v.keys {
		var expected []byte
		if key.scheme == "v1" {
			mac := hmac.New(sha256.New, key.material)
			_, _ = mac.Write(message)
			expected = mac.Sum(nil)
		}
		for _, signature := range signatures {
			if signature.scheme != key.scheme {
				continue
			}
			if key.scheme == "v1" {
				if hmac.Equal(expected, signature.data) {
					matched = true
				}
			} else {
				if ed25519.Verify(ed25519.PublicKey(key.material), message, signature.data) {
					matched = true
				}
			}
		}
	}
	if !matched {
		return Delivery[A]{}, InvalidSignature
	}
	if v.config.Protocol == Stripe {
		value, e := jsonwire.Decode(body, jsonwire.Limits{Bytes: int(v.config.MaxBodyBytes), Depth: 64, Nodes: 10000})
		if e != nil {
			return Delivery[A]{}, InvalidSignature
		}
		object, ok := value.(map[string]any)
		if !ok {
			return Delivery[A]{}, InvalidSignature
		}
		id, ok = object["id"].(string)
		if !ok || !deliveryID(id) {
			return Delivery[A]{}, InvalidSignature
		}
	}
	if err := ctx.Err(); err != nil {
		return Delivery[A]{}, err
	}
	return Delivery[A]{verifier: v, id: DeliveryID(id), timestamp: timestamp}, nil
}

type signature struct {
	scheme string
	data   []byte
}

func singleHeader(headers http.Header, name string, limit int) (string, error) {
	values := headers.Values(name)
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > limit {
		return "", InvalidSignature
	}
	return values[0], nil
}
func deliveryID(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
