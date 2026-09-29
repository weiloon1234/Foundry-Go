package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// DefaultPendingLifetime bounds the time between redirect and callback.
const DefaultPendingLifetime = 10 * time.Minute

// randomBytes is the entropy of state, nonce and PKCE verifier (256 bits).
const randomBytes = 32

// Pending is the server-held half of one authorization request: the state
// that binds the callback to this browser, the OpenID nonce bound into the ID
// token and the PKCE code verifier. Keep it server-side or in an encrypted,
// short-lived cookie (see Flow); never in a URL or plain cookie. Routine
// formatting, logging and JSON redact it.
type Pending struct {
	provider ProviderName
	state    string
	nonce    string
	verifier string
	expires  time.Time
}

func (Pending) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Pending) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Pending) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// Provider and ExpiresAt describe the pending request without its secrets.
func (p Pending) Provider() ProviderName { return p.provider }
func (p Pending) ExpiresAt() time.Time   { return p.expires }

func newToken() (string, error) {
	var raw [randomBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fault.Wrap(fault.Internal, "OAuth randomness failed", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func newPending(provider ProviderName, now time.Time, lifetime time.Duration) (Pending, error) {
	result := Pending{provider: provider, expires: now.Add(lifetime).UTC().Truncate(time.Second)}
	var err error
	for _, target := range []*string{&result.state, &result.nonce, &result.verifier} {
		if *target, err = newToken(); err != nil {
			return Pending{}, err
		}
	}
	return result, nil
}

// challenge is the PKCE S256 code challenge (RFC 7636).
func (p Pending) challenge() string {
	sum := sha256.Sum256([]byte(p.verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (p Pending) validate() error {
	if p.provider == "" || p.expires.IsZero() {
		return fault.New(fault.Invalid, "OAuth pending request is uninitialized")
	}
	for _, value := range []string{p.state, p.nonce, p.verifier} {
		if !canonicalToken(value) {
			return fault.New(fault.Invalid, "OAuth pending request is malformed")
		}
	}
	return nil
}

// matchesState compares the callback state in constant time.
func (p Pending) matchesState(state string) bool {
	return len(state) == len(p.state) && subtle.ConstantTimeCompare([]byte(state), []byte(p.state)) == 1
}

func canonicalToken(text string) bool {
	if len(text) != base64.RawURLEncoding.EncodedLen(randomBytes) {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(text)
	return err == nil && len(raw) == randomBytes
}

// pendingCodec is the cookie text form "v1:provider:state:nonce:verifier:expires",
// encrypted and authenticated by the cookie encrypter.
type pendingCodec struct{}

func (pendingCodec) Format(p Pending) (string, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	return strings.Join([]string{"v1", string(p.provider), p.state, p.nonce, p.verifier, strconv.FormatInt(p.expires.Unix(), 10)}, ":"), nil
}
func (pendingCodec) Parse(text string) (Pending, error) {
	parts := strings.Split(text, ":")
	if len(parts) != 6 || parts[0] != "v1" {
		return Pending{}, fault.New(fault.Invalid, "invalid OAuth pending request")
	}
	expires, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil || expires <= 0 {
		return Pending{}, fault.New(fault.Invalid, "invalid OAuth pending request")
	}
	result := Pending{provider: ProviderName(parts[1]), state: parts[2], nonce: parts[3], verifier: parts[4], expires: time.Unix(expires, 0).UTC()}
	return result, result.validate()
}
