package oauth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
)

const (
	// keyTTL bounds how long fetched signing keys are trusted before refetching.
	keyTTL = time.Hour
	// keyRefreshFloor bounds refetches for unknown key IDs (key rotation) so
	// forged kid values cannot turn every callback into a provider request.
	keyRefreshFloor = time.Minute
	maxKeys         = 64
	clockSkew       = time.Minute
	maxIDTokenBytes = 16 << 10
)

// publicKey is one verified-shape JWKS signing key for RS256 or ES256.
type publicKey struct {
	alg string
	rsa *rsa.PublicKey
	ec  *ecdsa.PublicKey
}

// keySet caches the provider's JWKS. One fetch runs at a time; concurrent
// verifications wait for it or their own context.
type keySet struct {
	mu      sync.Mutex
	keys    map[string]publicKey
	fetched time.Time
	loading chan struct{}
}

func newKeySet() *keySet { return &keySet{} }

func (s *keySet) find(kid, alg string) (publicKey, bool) {
	if kid != "" {
		key, ok := s.keys[kid]
		return key, ok && key.alg == alg
	}
	var match publicKey
	count := 0
	for _, key := range s.keys {
		if key.alg == alg {
			match, count = key, count+1
		}
	}
	return match, count == 1
}

// lookup returns the key for kid/alg, refetching the JWKS when the cache is
// stale or (at most once per keyRefreshFloor) when kid is unknown after rotation.
func (s *keySet) lookup(ctx context.Context, c *Client, kid, alg string) (publicKey, error) {
	for {
		s.mu.Lock()
		now := c.clock.Now()
		fresh := !s.fetched.IsZero() && now.Sub(s.fetched) < keyTTL && !now.Before(s.fetched)
		if fresh {
			if key, ok := s.find(kid, alg); ok {
				s.mu.Unlock()
				return key, nil
			}
			if now.Sub(s.fetched) < keyRefreshFloor {
				s.mu.Unlock()
				return publicKey{}, auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "ID token signing key is unknown"))
			}
		}
		if wait := s.loading; wait != nil {
			s.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return publicKey{}, ctx.Err()
			}
		}
		loading := make(chan struct{})
		s.loading = loading
		s.mu.Unlock()
		keys, err := c.fetchKeys(ctx)
		s.mu.Lock()
		s.loading = nil
		close(loading)
		if err == nil {
			s.keys, s.fetched = keys, c.clock.Now()
		}
		s.mu.Unlock()
		if err != nil {
			return publicKey{}, err
		}
	}
}

type jsonWebKey struct {
	KeyType string `json:"kty"`
	KeyID   string `json:"kid"`
	Use     string `json:"use"`
	Alg     string `json:"alg"`
	N       string `json:"n"`
	E       string `json:"e"`
	Curve   string `json:"crv"`
	X       string `json:"x"`
	Y       string `json:"y"`
}

// fetchKeys reads the JWKS and keeps signature keys this client can verify.
func (c *Client) fetchKeys(ctx context.Context) (map[string]publicKey, error) {
	response, err := c.http.Do(ctx, c.http.Get(c.provider.JWKSURL).Header("Accept", "application/json"))
	if err != nil {
		return nil, err
	}
	if err := response.EnsureSuccess(); err != nil {
		return nil, err
	}
	var document struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := decodeDocument(response.Bytes(), &document); err != nil {
		return nil, err
	}
	if len(document.Keys) > maxKeys {
		return nil, fault.New(fault.Invalid, "provider JWKS has too many keys")
	}
	keys := make(map[string]publicKey, len(document.Keys))
	for _, candidate := range document.Keys {
		if candidate.Use != "" && candidate.Use != "sig" {
			continue
		}
		key, ok := parseKey(candidate)
		if !ok {
			continue
		}
		if _, duplicate := keys[candidate.KeyID]; duplicate {
			return nil, fault.New(fault.Invalid, "provider JWKS repeats a key ID")
		}
		keys[candidate.KeyID] = key
	}
	return keys, nil
}

func decodeSegment(text string) ([]byte, bool) {
	data, err := base64.RawURLEncoding.Strict().DecodeString(text)
	return data, err == nil
}

// parseKey accepts RSA keys of at least 2048 bits (RS256) and P-256 keys (ES256).
func parseKey(key jsonWebKey) (publicKey, bool) {
	switch key.KeyType {
	case "RSA":
		if key.Alg != "" && key.Alg != "RS256" {
			return publicKey{}, false
		}
		modulus, ok := decodeSegment(key.N)
		if !ok || len(modulus) < 256 || len(modulus) > 1024 || modulus[0] == 0 {
			return publicKey{}, false
		}
		n := new(big.Int).SetBytes(modulus)
		if n.BitLen() < 2048 {
			return publicKey{}, false
		}
		exponent, ok := decodeSegment(key.E)
		if !ok || len(exponent) == 0 || len(exponent) > 4 {
			return publicKey{}, false
		}
		e := int(new(big.Int).SetBytes(exponent).Int64())
		if e < 3 || e%2 == 0 {
			return publicKey{}, false
		}
		return publicKey{alg: "RS256", rsa: &rsa.PublicKey{N: n, E: e}}, true
	case "EC":
		if key.Curve != "P-256" || key.Alg != "" && key.Alg != "ES256" {
			return publicKey{}, false
		}
		x, okX := decodeSegment(key.X)
		y, okY := decodeSegment(key.Y)
		if !okX || !okY || len(x) != 32 || len(y) != 32 {
			return publicKey{}, false
		}
		parsed, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return publicKey{}, false
		}
		return publicKey{alg: "ES256", ec: parsed}, true
	}
	return publicKey{}, false
}

type idHeader struct {
	Alg      string          `json:"alg"`
	KeyID    string          `json:"kid"`
	Critical json.RawMessage `json:"crit"`
}

type idClaims struct {
	Issuer          string          `json:"iss"`
	Subject         string          `json:"sub"`
	Audience        json.RawMessage `json:"aud"`
	AuthorizedParty string          `json:"azp"`
	Expiry          json.Number     `json:"exp"`
	IssuedAt        json.Number     `json:"iat"`
	Nonce           string          `json:"nonce"`
	Email           string          `json:"email"`
	EmailVerified   json.RawMessage `json:"email_verified"`
	Name            string          `json:"name"`
	Picture         string          `json:"picture"`
	Username        string          `json:"preferred_username"`
}

func rejected(message string) error {
	return auth.Unauthenticated.WithCause(fault.New(fault.Invalid, message))
}

// verifyIDToken checks the compact JWS signature with a JWKS key (RS256 or
// ES256 only; "none" and HMAC are rejected), then iss, aud/azp, exp, iat, the
// nonce bound at Begin and sub, and maps the standard claims to a Profile.
func (c *Client) verifyIDToken(ctx context.Context, token, nonce string) (Profile, error) {
	parts := strings.Split(token, ".")
	if len(token) > maxIDTokenBytes || len(parts) != 3 {
		return Profile{}, rejected("ID token is malformed")
	}
	headerJSON, ok := decodeSegment(parts[0])
	if !ok {
		return Profile{}, rejected("ID token header is malformed")
	}
	var header idHeader
	if err := decodeDocument(headerJSON, &header); err != nil || len(header.Critical) != 0 {
		return Profile{}, rejected("ID token header is malformed or has critical extensions")
	}
	if header.Alg != "RS256" && header.Alg != "ES256" {
		return Profile{}, rejected("ID token algorithm is not accepted")
	}
	signature, ok := decodeSegment(parts[2])
	if !ok {
		return Profile{}, rejected("ID token signature is malformed")
	}
	key, err := c.keys.lookup(ctx, c, header.KeyID, header.Alg)
	if err != nil {
		return Profile{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch key.alg {
	case "RS256":
		if rsa.VerifyPKCS1v15(key.rsa, crypto.SHA256, digest[:], signature) != nil {
			return Profile{}, rejected("ID token signature is invalid")
		}
	case "ES256":
		if len(signature) != 64 || !ecdsa.Verify(key.ec, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
			return Profile{}, rejected("ID token signature is invalid")
		}
	default:
		return Profile{}, rejected("ID token algorithm is not accepted")
	}
	payload, ok := decodeSegment(parts[1])
	if !ok {
		return Profile{}, rejected("ID token payload is malformed")
	}
	var claims idClaims
	if err := decodeDocument(payload, &claims); err != nil {
		return Profile{}, rejected("ID token payload is malformed")
	}
	now := c.clock.Now()
	if !slices.Contains(c.provider.Issuers, claims.Issuer) {
		return Profile{}, rejected("ID token issuer is not accepted")
	}
	clientID := c.provider.Credentials.ClientID
	audiences, err := audienceList(claims.Audience)
	if err != nil || !slices.Contains(audiences, clientID) || (len(audiences) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != clientID {
		return Profile{}, rejected("ID token audience is not this client")
	}
	expiry, errExpiry := numericDate(claims.Expiry)
	issued, errIssued := numericDate(claims.IssuedAt)
	if errExpiry != nil || errIssued != nil || !now.Before(expiry.Add(clockSkew)) || issued.After(now.Add(clockSkew)) {
		return Profile{}, rejected("ID token is expired or not yet valid")
	}
	if len(claims.Nonce) != len(nonce) || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		return Profile{}, rejected("ID token nonce does not match the request")
	}
	if claims.Subject == "" || len(claims.Subject) > 255 {
		return Profile{}, rejected("ID token has no subject")
	}
	verified := string(claims.EmailVerified) == "true" || string(claims.EmailVerified) == `"true"`
	return Profile{Subject: claims.Subject, Email: claims.Email, EmailVerified: verified && claims.Email != "", Name: claims.Name, Username: claims.Username, AvatarURL: claims.Picture}, nil
}

func audienceList(raw json.RawMessage) ([]string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil || len(many) == 0 || len(many) > 16 {
		return nil, fault.New(fault.Invalid, "invalid audience")
	}
	return many, nil
}

// numericDate parses a JWT NumericDate (seconds since the epoch, possibly fractional).
func numericDate(value json.Number) (time.Time, error) {
	seconds, err := strconv.ParseFloat(value.String(), 64)
	if err != nil || seconds <= 0 || seconds > 1e11 {
		return time.Time{}, fault.New(fault.Invalid, "invalid numeric date")
	}
	whole := math.Floor(seconds)
	return time.Unix(int64(whole), int64((seconds-whole)*float64(time.Second))).UTC(), nil
}
