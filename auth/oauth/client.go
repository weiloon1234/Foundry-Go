package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Denied is the cause of an auth.Unauthenticated callback that carried a
// provider error such as access_denied (the user declined).
const Denied failureCode = "oauth authorization was denied at the provider"

type failureCode string

func (c failureCode) Error() string { return string(c) }

// maxProviderResponseBytes bounds token, user and JWKS documents.
const maxProviderResponseBytes = 256 << 10

// Client runs one provider's authorization-code flow. It is immutable and
// safe for concurrent use; the JWKS cache is shared by its calls.
type Client struct {
	provider Provider
	http     *httpclient.Client
	clock    clock.Clock
	lifetime time.Duration
	keys     *keySet
}

// NewClient validates the provider and borrows an outbound client that must
// restrict destinations (see Provider.Destinations), so provider URLs cannot
// be used to reach internal services. The client must have no BaseURL.
func NewClient(provider Provider, outbound *httpclient.Client, source clock.Clock) (*Client, error) {
	provider = provider.snapshot()
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	if outbound == nil || !outbound.RestrictsDestinations() {
		return nil, fault.New(fault.Invalid, "OAuth requires an httpclient with restricted destinations")
	}
	if credential.IsNil(source) {
		return nil, fault.New(fault.Invalid, "OAuth requires a clock")
	}
	return &Client{provider: provider, http: outbound, clock: source, lifetime: DefaultPendingLifetime, keys: newKeySet()}, nil
}

func (c *Client) Validate() error {
	if c == nil || c.http == nil || c.keys == nil {
		return fault.New(fault.Invalid, "OAuth client is not configured")
	}
	return nil
}

// Provider returns the immutable provider declaration.
func (c *Client) Provider() Provider { return c.provider.snapshot() }

// Authorization is a started request: redirect the browser to URL and keep
// Pending until the callback.
type Authorization struct {
	URL     string
	Pending Pending
}

func (Authorization) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("OAuth authorization")) }
func (Authorization) LogValue() slog.Value       { return slog.StringValue("OAuth authorization") }

// Begin creates fresh state, nonce and PKCE verifier and the provider URL with
// response_type=code, the exact redirect URI and the S256 code challenge.
func (c *Client) Begin(ctx context.Context) (Authorization, error) {
	if err := c.Validate(); err != nil {
		return Authorization{}, err
	}
	if err := ctx.Err(); err != nil {
		return Authorization{}, err
	}
	pending, err := newPending(c.provider.Name, c.clock.Now(), c.lifetime)
	if err != nil {
		return Authorization{}, err
	}
	target, err := endpoint(c.provider.AuthorizationURL)
	if err != nil {
		return Authorization{}, err
	}
	query := target.Query()
	query.Set("response_type", "code")
	query.Set("client_id", c.provider.Credentials.ClientID)
	query.Set("redirect_uri", c.provider.Credentials.RedirectURL)
	query.Set("scope", strings.Join(c.provider.Scopes, " "))
	query.Set("state", pending.state)
	query.Set("code_challenge", pending.challenge())
	query.Set("code_challenge_method", "S256")
	if c.provider.Source == OpenIDConnect {
		query.Set("nonce", pending.nonce)
	}
	target.RawQuery = query.Encode()
	return Authorization{URL: target.String(), Pending: pending}, nil
}

// Callback is the provider's redirect query.
type Callback struct {
	Code  secret.String
	State string
	// Error is the provider's error code, for example access_denied.
	Error string
}

// ParseCallback reads code, state and error from a callback query, rejecting
// repeated or oversized parameters.
func ParseCallback(query url.Values) (Callback, error) {
	invalid := auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "malformed OAuth callback"))
	single := func(name string, limit int) (string, error) {
		values := query[name]
		if len(values) > 1 || len(values) == 1 && (len(values[0]) > limit || strings.ContainsFunc(values[0], controlOrSpace)) {
			return "", invalid
		}
		if len(values) == 0 {
			return "", nil
		}
		return values[0], nil
	}
	code, err := single("code", 4096)
	if err != nil {
		return Callback{}, err
	}
	state, err := single("state", 256)
	if err != nil {
		return Callback{}, err
	}
	failure, err := single("error", 128)
	if err != nil {
		return Callback{}, err
	}
	return Callback{Code: secret.New(code), State: state, Error: failure}, nil
}

// Complete validates a callback against its pending request (provider, expiry
// and state), exchanges the code with the PKCE verifier and reads the profile.
// OpenID Connect ID tokens are verified (signature, issuer, audience, expiry,
// issue time and nonce). Every rejected callback returns auth.Unauthenticated;
// provider and network failures remain operational errors. The pending request
// is single use: discard it after this call, whatever the result.
func (c *Client) Complete(ctx context.Context, pending Pending, callback Callback) (Profile, error) {
	if err := c.Validate(); err != nil {
		return Profile{}, err
	}
	if err := pending.validate(); err != nil {
		return Profile{}, auth.Unauthenticated.WithCause(err)
	}
	now := c.clock.Now()
	if pending.provider != c.provider.Name || !now.Before(pending.expires) || !pending.matchesState(callback.State) {
		return Profile{}, auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "OAuth callback does not match its pending request"))
	}
	if callback.Error != "" {
		return Profile{}, auth.Unauthenticated.WithCause(Denied)
	}
	if callback.Code.IsZero() {
		return Profile{}, auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "OAuth callback has no code"))
	}
	tokens, idToken, err := c.exchange(ctx, pending, callback.Code)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	switch c.provider.Source {
	case OpenIDConnect:
		profile, err = c.verifyIDToken(ctx, idToken, pending.nonce)
	case GitHubUser:
		profile, err = c.githubProfile(ctx, tokens.Access)
	}
	if err != nil {
		return Profile{}, err
	}
	profile.Provider, profile.Token = c.provider.Name, tokens
	return profile, nil
}

type tokenResponse struct {
	AccessToken  string      `json:"access_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    json.Number `json:"expires_in"`
	RefreshToken string      `json:"refresh_token"`
	IDToken      string      `json:"id_token"`
	Scope        string      `json:"scope"`
	Error        string      `json:"error"`
}

// exchange posts the authorization code with the verifier, the exact redirect
// URI and client_secret_post credentials, and validates the token response.
func (c *Client) exchange(ctx context.Context, pending Pending, code secret.String) (Token, string, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code.Reveal()}, "redirect_uri": {c.provider.Credentials.RedirectURL}, "client_id": {c.provider.Credentials.ClientID}, "code_verifier": {pending.verifier}}
	if !c.provider.Credentials.ClientSecret.IsZero() {
		form.Set("client_secret", c.provider.Credentials.ClientSecret.Reveal())
	}
	request := c.http.Post(c.provider.TokenURL).Form(form).Header("Accept", "application/json")
	response, err := c.http.Do(ctx, request)
	if err != nil {
		return Token{}, "", err
	}
	if status := response.Status(); status >= 300 && status != 400 && status != 401 {
		return Token{}, "", response.EnsureSuccess()
	}
	var decoded tokenResponse
	if err := decodeDocument(response.Bytes(), &decoded); err != nil {
		return Token{}, "", err
	}
	if decoded.Error != "" || response.Status() != 200 {
		// The code or verifier was rejected: an invalid or replayed callback.
		return Token{}, "", auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "OAuth token endpoint rejected the authorization code"))
	}
	if decoded.AccessToken == "" || len(decoded.AccessToken) > 16384 || !strings.EqualFold(decoded.TokenType, "bearer") {
		return Token{}, "", fault.New(fault.Invalid, "OAuth token response is malformed")
	}
	result := Token{Access: secret.New(decoded.AccessToken), Scopes: strings.Fields(strings.ReplaceAll(decoded.Scope, ",", " "))}
	if decoded.RefreshToken != "" {
		result.Refresh = value.Set(secret.New(decoded.RefreshToken))
	}
	if decoded.ExpiresIn != "" {
		seconds, err := strconv.ParseInt(decoded.ExpiresIn.String(), 10, 64)
		if err != nil || seconds <= 0 || seconds > 366*24*3600 {
			return Token{}, "", fault.New(fault.Invalid, "OAuth token expiry is malformed")
		}
		result.ExpiresAt = value.Set(c.clock.Now().Add(time.Duration(seconds) * time.Second).UTC())
	}
	if c.provider.Source == OpenIDConnect && decoded.IDToken == "" {
		return Token{}, "", fault.New(fault.Invalid, "OpenID Connect token response has no ID token")
	}
	return result, decoded.IDToken, nil
}

// decodeDocument decodes one bounded JSON document from a provider.
func decodeDocument(data []byte, target any) error {
	if len(data) == 0 || len(data) > maxProviderResponseBytes {
		return fault.New(fault.Invalid, "OAuth provider response is empty or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fault.Wrap(fault.Invalid, "OAuth provider response is malformed", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fault.New(fault.Invalid, "OAuth provider response has trailing data")
	}
	return nil
}
