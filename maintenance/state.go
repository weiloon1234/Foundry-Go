package maintenance

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const (
	// MaxRules bounds configured plus shared exemption rules separately.
	MaxRules = 64
	// MaxAllowedPrefixes bounds configured plus shared network allow lists separately.
	MaxAllowedPrefixes = 64
	// MaxMessageBytes bounds the operator message rendered in maintenance responses.
	MaxMessageBytes = 512
	// MaxRetryAfter bounds the advertised Retry-After interval.
	MaxRetryAfter = 24 * time.Hour
	// DefaultBypassTTL is the lifetime of a secret bypass cookie.
	DefaultBypassTTL = 12 * time.Hour
	// MaxBypassTTL bounds configured bypass cookie lifetimes.
	MaxBypassTTL = 7 * 24 * time.Hour
	// BypassCookie names the signed cookie that admits a browser holding the secret.
	BypassCookie = "foundry_maintenance"
	stateVersion = 1
)

// Rule exempts matching requests from a paused gate. Method is an uppercase
// HTTP method or empty for every method. Path is a canonical absolute path;
// Prefix also matches every descendant segment ("/status" matches
// "/status/db", never "/statuses"). Exemptions never bypass Draining.
type Rule struct {
	Method string `json:"method,omitempty"`
	Path   string `json:"path"`
	Prefix bool   `json:"prefix,omitempty"`
}

func (r Rule) Validate() error {
	if len(r.Method) > 16 || strings.IndexFunc(r.Method, func(c rune) bool { return c < 'A' || c > 'Z' }) >= 0 {
		return fault.New(fault.Invalid, "maintenance exemption method must be an uppercase HTTP method")
	}
	if !validPath(r.Path) {
		return fault.New(fault.Invalid, "maintenance exemption path must be a canonical absolute path")
	}
	return nil
}

// Matches compares an unescaped request path; callers reject escaped paths.
func (r Rule) Matches(method, path string) bool {
	if r.Method != "" && r.Method != method {
		return false
	}
	if path == r.Path {
		return true
	}
	return r.Prefix && strings.HasPrefix(path, strings.TrimSuffix(r.Path, "/")+"/")
}

func validPath(path string) bool {
	if path == "/" {
		return true
	}
	if len(path) < 2 || len(path) > 1024 || path[0] != '/' || strings.HasSuffix(path, "/") || !utf8.ValidString(path) {
		return false
	}
	for _, segment := range strings.Split(path[1:], "/") {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "?#%\\") || strings.IndexFunc(segment, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

// Policy is deployment configuration retained by one Gate. It never changes
// after construction; shared operator State adds its own rules and networks.
type Policy struct {
	Exempt    []Rule
	Allow     []netip.Prefix
	BypassTTL time.Duration
	// Keys seal bypass cookies so every instance sharing them accepts one;
	// typically the application key ring. Without keys a cookie is valid only
	// on the instance that issued it.
	Keys *encryption.Keyring
}

func (p Policy) Validate() error {
	if err := validateRules(p.Exempt, p.Allow); err != nil {
		return err
	}
	if p.Keys != nil {
		if err := p.Keys.Validate(); err != nil {
			return err
		}
	}
	if p.BypassTTL < 0 || p.BypassTTL > MaxBypassTTL || p.BypassTTL%time.Second != 0 {
		return fault.New(fault.Invalid, "maintenance bypass lifetime must be whole seconds up to seven days")
	}
	return nil
}

func (p Policy) snapshot() Policy {
	p.Exempt = slices.Clone(p.Exempt)
	p.Allow = slices.Clone(p.Allow)
	if p.BypassTTL == 0 {
		p.BypassTTL = DefaultBypassTTL
	}
	return p
}

func validateRules(rules []Rule, allow []netip.Prefix) error {
	if len(rules) > MaxRules || len(allow) > MaxAllowedPrefixes {
		return fault.New(fault.Invalid, "maintenance exemptions exceed their bound")
	}
	seen := make(map[Rule]bool, len(rules))
	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if seen[rule] {
			return fault.New(fault.Duplicate, "duplicate maintenance exemption")
		}
		seen[rule] = true
	}
	for _, prefix := range allow {
		if !prefix.IsValid() || prefix != prefix.Masked() || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
			return fault.New(fault.Invalid, "maintenance allowed networks must be canonical CIDR prefixes")
		}
	}
	return nil
}

// SecretDigest is the SHA-256 digest of a bypass secret. Stores retain only the
// digest; the plaintext secret is shown once to the operator who created it.
type SecretDigest [32]byte

func (d SecretDigest) IsZero() bool { return d == SecretDigest{} }
func (d SecretDigest) MarshalText() ([]byte, error) {
	if d.IsZero() {
		return []byte{}, nil
	}
	return []byte(hex.EncodeToString(d[:])), nil
}
func (d *SecretDigest) UnmarshalText(text []byte) error {
	if d == nil {
		return fault.New(fault.Invalid, "missing maintenance secret digest destination")
	}
	if len(text) == 0 {
		*d = SecretDigest{}
		return nil
	}
	var decoded SecretDigest
	if len(text) != 64 || strings.ToLower(string(text)) != string(text) {
		return fault.New(fault.Invalid, "invalid maintenance secret digest")
	}
	if _, err := hex.Decode(decoded[:], text); err != nil {
		return fault.New(fault.Invalid, "invalid maintenance secret digest")
	}
	*d = decoded
	return nil
}
func (SecretDigest) LogValue() slog.Value { return slog.StringValue("maintenance secret digest") }

// NewSecret returns a random URL-safe bypass secret for an operator.
func NewSecret() (string, error) {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fault.Wrap(fault.Internal, "cannot generate maintenance secret", err)
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

// DigestSecret validates a bypass secret: 16 to 128 URL-safe characters
// (letters, digits, '-' and '_'), usable as one request path segment.
func DigestSecret(secret string) (SecretDigest, error) {
	if len(secret) < 16 || len(secret) > 128 || strings.IndexFunc(secret, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_')
	}) >= 0 {
		return SecretDigest{}, fault.New(fault.Invalid, "maintenance secret needs 16 to 128 URL-safe characters")
	}
	return sha256.Sum256([]byte(secret)), nil
}

// State is the operator-controlled maintenance record shared by a fleet. Down
// pauses admission. RetryAfter is advertised in whole seconds; Message is an
// operator-supplied public text. Allow and Exempt add to the configured Policy.
type State struct {
	Down       bool
	RetryAfter time.Duration
	Message    string
	Secret     SecretDigest
	Allow      []netip.Prefix
	Exempt     []Rule
	Since      time.Time
}

func (s State) Validate() error {
	if s.RetryAfter < 0 || s.RetryAfter > MaxRetryAfter || s.RetryAfter%time.Second != 0 {
		return fault.New(fault.Invalid, "maintenance retry interval must be whole seconds up to 24 hours")
	}
	if len(s.Message) > MaxMessageBytes || !utf8.ValidString(s.Message) || strings.IndexFunc(s.Message, unicode.IsControl) >= 0 {
		return fault.New(fault.Invalid, "maintenance message must be bounded single-line text")
	}
	if !s.Down && (s.RetryAfter != 0 || s.Message != "" || !s.Secret.IsZero() || len(s.Allow) != 0 || len(s.Exempt) != 0) {
		return fault.New(fault.Invalid, "serving maintenance state cannot carry maintenance options")
	}
	return validateRules(s.Exempt, s.Allow)
}

func (s State) snapshot() State {
	s.Allow = slices.Clone(s.Allow)
	s.Exempt = slices.Clone(s.Exempt)
	return s
}

type wireState struct {
	Version    int            `json:"v"`
	Down       bool           `json:"down"`
	RetryAfter int64          `json:"retry_after_seconds,omitempty"`
	Message    string         `json:"message,omitempty"`
	Secret     SecretDigest   `json:"secret_sha256,omitzero"`
	Allow      []netip.Prefix `json:"allow,omitempty"`
	Exempt     []Rule         `json:"exempt,omitempty"`
	Since      time.Time      `json:"since,omitzero"`
}

// MarshalJSON is the versioned shared-store representation. It contains the
// secret digest, never the secret itself.
func (s State) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(wireState{Version: stateVersion, Down: s.Down, RetryAfter: int64(s.RetryAfter / time.Second), Message: s.Message, Secret: s.Secret, Allow: s.Allow, Exempt: s.Exempt, Since: s.Since.UTC()})
}

func (s *State) UnmarshalJSON(data []byte) error {
	if s == nil {
		return fault.New(fault.Invalid, "missing maintenance state destination")
	}
	var wire wireState
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if len(data) > 64<<10 || decoder.Decode(&wire) != nil || decoder.More() {
		return fault.New(fault.Invalid, "invalid maintenance state record")
	}
	if wire.Version != stateVersion || wire.RetryAfter < 0 || wire.RetryAfter > int64(MaxRetryAfter/time.Second) {
		return fault.New(fault.Invalid, "unsupported maintenance state record")
	}
	decoded := State{Down: wire.Down, RetryAfter: time.Duration(wire.RetryAfter) * time.Second, Message: wire.Message, Secret: wire.Secret, Allow: wire.Allow, Exempt: wire.Exempt, Since: wire.Since}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*s = decoded
	return nil
}

// MatchesSecret compares a candidate secret with the stored digest in constant time.
func (s State) MatchesSecret(candidate string) bool {
	if s.Secret.IsZero() {
		return false
	}
	digest, err := DigestSecret(candidate)
	return err == nil && subtle.ConstantTimeCompare(digest[:], s.Secret[:]) == 1
}

// bypassPurpose separates bypass tokens from other application envelopes.
const bypassPurpose encryption.Purpose = "foundry.maintenance.bypass.v1"

// sealBypass signs an expiry for the current secret digest. With application
// keys the value is an authenticated envelope bound to the digest, valid on
// every instance sharing those keys; the shared store holds only the digest,
// so reading it never lets anyone mint a bypass. Without keys it is an HMAC
// under local, a key that never leaves this process.
func sealBypass(ctx context.Context, keys *encryption.Keyring, local []byte, digest SecretDigest, expires time.Time) (string, error) {
	expiry := uint64(expires.Unix())
	if keys == nil {
		data := binary.BigEndian.AppendUint64(make([]byte, 0, 8+sha256.Size), expiry)
		return base64.RawURLEncoding.EncodeToString(append(data, localBypassMAC(local, digest, expiry)...)), nil
	}
	binding, err := encryption.NewContext(bypassPurpose, secret.New(hex.EncodeToString(digest[:])))
	if err != nil {
		return "", err
	}
	sealed, err := keys.Encrypt(ctx, binding, secret.New(strconv.FormatUint(expiry, 10)))
	if err != nil {
		return "", err
	}
	return sealed.Encoded(), nil
}

// openBypass accepts an unexpired value sealed for the current secret digest
// by the same key source.
func openBypass(ctx context.Context, keys *encryption.Keyring, local []byte, digest SecretDigest, value string, now time.Time) bool {
	var expiry uint64
	if keys == nil {
		if len(value) != base64.RawURLEncoding.EncodedLen(8+sha256.Size) {
			return false
		}
		data, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil || len(data) != 8+sha256.Size {
			return false
		}
		expiry = binary.BigEndian.Uint64(data[:8])
		if !hmac.Equal(data[8:], localBypassMAC(local, digest, expiry)) {
			return false
		}
	} else {
		sealed, err := encryption.ParseCiphertext(value)
		if err != nil {
			return false
		}
		binding, err := encryption.NewContext(bypassPurpose, secret.New(hex.EncodeToString(digest[:])))
		if err != nil {
			return false
		}
		plaintext, err := keys.Decrypt(ctx, binding, sealed)
		if err != nil {
			return false
		}
		if expiry, err = strconv.ParseUint(plaintext.Reveal(), 10, 64); err != nil {
			return false
		}
	}
	return expiry <= uint64(now.Add(MaxBypassTTL).Unix()) && int64(expiry) > now.Unix()
}

func localBypassMAC(key []byte, digest SecretDigest, expiry uint64) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("foundry.maintenance.bypass.v1\x00"))
	mac.Write(digest[:])
	mac.Write(binary.BigEndian.AppendUint64(nil, expiry))
	return mac.Sum(nil)
}

// ParseRule reads the operator text form "[METHOD ]/path[/*]". A trailing
// "/*" selects prefix matching ("/*" alone exempts every path).
func ParseRule(text string) (Rule, error) {
	var rule Rule
	method, path, spaced := strings.Cut(text, " ")
	if spaced {
		rule.Method = method
	} else {
		path = method
	}
	if path == "/*" {
		path, rule.Prefix = "/", true
	} else if trimmed, ok := strings.CutSuffix(path, "/*"); ok {
		path, rule.Prefix = trimmed, true
	}
	rule.Path = path
	if err := rule.Validate(); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// String returns the ParseRule text form.
func (r Rule) String() string {
	text := r.Path
	if r.Prefix {
		text = strings.TrimSuffix(text, "/") + "/*"
	}
	if r.Method != "" {
		text = r.Method + " " + text
	}
	return text
}
