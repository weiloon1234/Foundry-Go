// Package webhook verifies bounded original webhook bytes before typed decoding.
// Verified identities are bound to a configured provider/account and are not a
// replacement for current domain authorization or durable idempotency.
package webhook

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha1" // Legacy HMAC-SHA1 provider signatures only; never hashing secrets.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
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
	// HMAC is a configurable single-header HMAC signature (Config.HMAC); see
	// the GitHub, Shopify and Slack presets.
	HMAC Protocol = "hmac"
)

// Verification work is bounded per request: at most maxSignatures distinct
// supported signatures are considered, and at most maxAsymmetricChecks Ed25519
// verifications (each hashes the complete body) run before failing closed.
const (
	maxSignatures       = 8
	maxAsymmetricChecks = 8
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
	// HMAC configures the HMAC protocol and must be zero for the others.
	HMAC HMACScheme
}

func DefaultConfig(provider Provider, protocol Protocol) Config {
	return Config{Provider: provider, Protocol: protocol, MaxBodyBytes: 1 << 20, Tolerance: 5 * time.Minute}
}
func (c Config) Validate() error {
	invalid := fault.New(fault.Invalid, "invalid webhook verification configuration")
	if !identifier.Semantic(string(c.Provider)) || c.Protocol != Standard && c.Protocol != Stripe && c.Protocol != HMAC || c.MaxBodyBytes < 1 || c.MaxBodyBytes > 16<<20 || c.Tolerance < time.Second || c.Tolerance > time.Hour {
		return invalid
	}
	if c.Protocol != HMAC {
		if c.HMAC != (HMACScheme{}) {
			return invalid
		}
		return nil
	}
	return c.HMAC.validate()
}

type HMACAlgorithm string
type SignatureEncoding string

const (
	SHA256 HMACAlgorithm = "sha256"
	SHA512 HMACAlgorithm = "sha512"
	// SHA1 exists only for legacy providers; prefer SHA256 when offered.
	SHA1 HMACAlgorithm = "sha1"

	Hex    SignatureEncoding = "hex"
	Base64 SignatureEncoding = "base64"
)

// HMACScheme describes a provider's HMAC signature. SignatureHeader carries one
// signature, optionally after Prefix (for example "sha256="). The signed
// content is the exact body, or, with TimestampHeader, PayloadPrefix +
// timestamp + Separator + body (Slack signs "v0:" + timestamp + ":" + body).
// A timestamp is Unix seconds checked against Config.Tolerance; without one no
// freshness check is possible. The delivery ID is always "sha256-" + the
// digest of the verified signed content, because these schemes sign no ID:
// durable idempotency then rejects replays of identical signed content, but a
// provider that legitimately resends an identical body without a signed
// timestamp is indistinguishable from a replay. DeliveryHeader names an
// optional provider delivery header exposed only as
// Delivery.UnverifiedProviderID. Endpoint secrets are used verbatim as HMAC keys.
type HMACScheme struct {
	SignatureHeader string
	Prefix          string
	Algorithm       HMACAlgorithm
	Encoding        SignatureEncoding
	TimestampHeader string
	PayloadPrefix   string
	Separator       string
	DeliveryHeader  string
}

func (h HMACScheme) validate() error {
	invalid := fault.New(fault.Invalid, "invalid webhook HMAC scheme")
	if !headerName(h.SignatureHeader) || h.TimestampHeader != "" && !headerName(h.TimestampHeader) || h.DeliveryHeader != "" && !headerName(h.DeliveryHeader) {
		return invalid
	}
	if h.Algorithm != SHA256 && h.Algorithm != SHA512 && h.Algorithm != SHA1 || h.Encoding != Hex && h.Encoding != Base64 {
		return invalid
	}
	if h.TimestampHeader == "" && (h.PayloadPrefix != "" || h.Separator != "") || len(h.Prefix) > 32 || len(h.PayloadPrefix) > 32 || len(h.Separator) > 8 {
		return invalid
	}
	return nil
}
func (h HMACScheme) hash() func() hash.Hash {
	switch h.Algorithm {
	case SHA512:
		return sha512.New
	case SHA1:
		return sha1.New
	default:
		return sha256.New
	}
}
func headerName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// GitHubConfig verifies X-Hub-Signature-256 (hex HMAC-SHA256 of the body).
// GitHub signs no timestamp or ID: the delivery ID is the body digest and
// X-GitHub-Delivery is unverified metadata.
func GitHubConfig(provider Provider) Config {
	c := DefaultConfig(provider, HMAC)
	c.HMAC = HMACScheme{SignatureHeader: "X-Hub-Signature-256", Prefix: "sha256=", Algorithm: SHA256, Encoding: Hex, DeliveryHeader: "X-GitHub-Delivery"}
	return c
}

// ShopifyConfig verifies X-Shopify-Hmac-Sha256 (base64 HMAC-SHA256 of the
// body). Shopify signs no timestamp or ID: the delivery ID is the body digest
// and X-Shopify-Webhook-Id is unverified metadata.
func ShopifyConfig(provider Provider) Config {
	c := DefaultConfig(provider, HMAC)
	c.HMAC = HMACScheme{SignatureHeader: "X-Shopify-Hmac-Sha256", Algorithm: SHA256, Encoding: Base64, DeliveryHeader: "X-Shopify-Webhook-Id"}
	return c
}

// SlackConfig verifies X-Slack-Signature ("v0=" hex HMAC-SHA256 of
// "v0:{X-Slack-Request-Timestamp}:{body}") with freshness checking. Slack
// sends no delivery header; the delivery ID digests the signed timestamp and body.
func SlackConfig(provider Provider) Config {
	c := DefaultConfig(provider, HMAC)
	c.HMAC = HMACScheme{SignatureHeader: "X-Slack-Signature", Prefix: "v0=", Algorithm: SHA256, Encoding: Hex, TimestampHeader: "X-Slack-Request-Timestamp", PayloadPrefix: "v0:", Separator: ":"}
	return c
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
// Stripe endpoint secrets, or HMAC-protocol secrets (both used verbatim). No
// keys are loaded from requests.
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
		if config.Protocol == HMAC {
			if len(text) < 16 {
				return nil, fault.New(fault.Invalid, "invalid webhook HMAC secret")
			}
			item.material = []byte(text)
		} else if config.Protocol == Stripe {
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
	verifier   *Verifier[A]
	id         DeliveryID
	timestamp  time.Time
	unverified string
}

func (d Delivery[A]) ID() DeliveryID { return d.id }

// UnverifiedProviderID returns the provider's delivery header of an HMAC
// scheme (for example X-GitHub-Delivery). The signature does not cover it, so
// it is diagnostic metadata only: never use it for deduplication or
// authorization. ID is the authenticated identity.
func (d Delivery[A]) UnverifiedProviderID() (string, bool) {
	return d.unverified, d.unverified != ""
}
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
	var id, stamp, unverified string
	var signatures []signature
	var err error
	var message []byte
	switch v.config.Protocol {
	case Standard:
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
			signatures = appendSignature(signatures, signature{scheme, data})
		}
	case Stripe:
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
				signatures = appendSignature(signatures, signature{"v1", data})
			}
		}
	case HMAC:
		scheme := v.config.HMAC
		text, e := singleHeader(headers, scheme.SignatureHeader, 1024)
		if e != nil {
			return Delivery[A]{}, InvalidSignature
		}
		encoded, ok := strings.CutPrefix(text, scheme.Prefix)
		if !ok {
			return Delivery[A]{}, InvalidSignature
		}
		var data []byte
		if scheme.Encoding == Base64 {
			data, e = base64.StdEncoding.Strict().DecodeString(encoded)
		} else {
			data, e = hex.DecodeString(encoded)
		}
		if e != nil {
			return Delivery[A]{}, InvalidSignature
		}
		signatures = appendSignature(signatures, signature{"v1", data})
		if scheme.TimestampHeader != "" {
			if stamp, e = singleHeader(headers, scheme.TimestampHeader, 20); e != nil {
				return Delivery[A]{}, InvalidSignature
			}
			message = make([]byte, 0, len(scheme.PayloadPrefix)+len(stamp)+len(scheme.Separator)+len(body))
			message = append(append(append(message, scheme.PayloadPrefix...), stamp...), scheme.Separator...)
		}
		if scheme.DeliveryHeader != "" {
			if unverified, e = singleHeader(headers, scheme.DeliveryHeader, 256); e != nil || !deliveryID(unverified) {
				return Delivery[A]{}, InvalidSignature
			}
		}
	}
	var timestamp time.Time
	if v.config.Protocol != HMAC || stamp != "" {
		seconds, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil || seconds <= 0 || strconv.FormatInt(seconds, 10) != stamp {
			return Delivery[A]{}, InvalidSignature
		}
		timestamp = time.Unix(seconds, 0).UTC()
		var now time.Time
		if err := callback.Isolated("webhook clock", func() error { now = v.clock.Now().UTC(); return nil }); err != nil {
			return Delivery[A]{}, err
		}
		if now.IsZero() || timestamp.Before(now.Add(-v.config.Tolerance)) || timestamp.After(now.Add(v.config.Tolerance)) {
			return Delivery[A]{}, InvalidSignature
		}
	}
	switch v.config.Protocol {
	case Standard:
		message = append(append(make([]byte, 0, len(id)+len(stamp)+2+len(body)), id+"."+stamp+"."...), body...)
	case Stripe:
		message = append(append(make([]byte, 0, len(stamp)+1+len(body)), stamp+"."...), body...)
	default:
		message = append(message, body...)
	}
	if !v.matches(message, signatures) {
		return Delivery[A]{}, InvalidSignature
	}
	if v.config.Protocol == HMAC {
		// HMAC schemes never sign a delivery ID. The durable identity is the
		// digest of the verified signed content (the body, plus the signed
		// timestamp when the scheme has one), so replaying a captured request
		// with a fresh provider header still maps to the same ID.
		digest := sha256.Sum256(message)
		id = "sha256-" + hex.EncodeToString(digest[:])
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
	return Delivery[A]{verifier: v, id: DeliveryID(id), timestamp: timestamp, unverified: unverified}, nil
}

type signature struct {
	scheme string
	data   []byte
}

// appendSignature ignores duplicates and signatures beyond the checked bound.
func appendSignature(signatures []signature, next signature) []signature {
	if len(signatures) >= maxSignatures {
		return signatures
	}
	for _, existing := range signatures {
		if existing.scheme == next.scheme && bytes.Equal(existing.data, next.data) {
			return signatures
		}
	}
	return append(signatures, next)
}

// matches stops at the first valid signature. Each HMAC key is computed once
// and compared in constant time; Ed25519 checks run only for correctly sized
// signatures and are bounded, because each one hashes the complete body.
func (v *Verifier[A]) matches(message []byte, signatures []signature) bool {
	asymmetric := 0
	for _, key := range v.keys {
		var expected []byte
		for _, candidate := range signatures {
			if candidate.scheme != key.scheme {
				continue
			}
			if key.scheme == "v1" {
				if expected == nil {
					newHash := sha256.New
					if v.config.Protocol == HMAC {
						newHash = v.config.HMAC.hash()
					}
					mac := hmac.New(newHash, key.material)
					_, _ = mac.Write(message)
					expected = mac.Sum(nil)
				}
				if hmac.Equal(expected, candidate.data) {
					return true
				}
				continue
			}
			if len(candidate.data) != ed25519.SignatureSize {
				continue
			}
			if asymmetric++; asymmetric > maxAsymmetricChecks {
				return false
			}
			if ed25519.Verify(ed25519.PublicKey(key.material), message, candidate.data) {
				return true
			}
		}
	}
	return false
}

func singleHeader(headers http.Header, name string, limit int) (string, error) {
	values := headers.Values(name)
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > limit {
		return "", InvalidSignature
	}
	return values[0], nil
}

// deliveryID accepts visible ASCII except '.', which delimits Standard
// Webhooks signed content ("id.timestamp.body"), and ',' used by signature
// lists. Provider IDs such as UUIDs, KSUIDs and "msg_…" values are accepted.
func deliveryID(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, c := range []byte(value) {
		if c < 0x21 || c > 0x7e || c == '.' || c == ',' {
			return false
		}
	}
	return true
}
