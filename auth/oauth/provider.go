// Package oauth is an OAuth 2.0 authorization-code client with PKCE and OpenID
// Connect ID-token verification for social login. It returns a typed Profile;
// linking or creating the application's own user is the application's decision.
// Provider requests use an SSRF-restricted httpclient.Client. No dependency
// beyond the standard library cryptography is used.
package oauth

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// ProviderName identifies a configured provider, for example "google".
type ProviderName string

// Credentials are the application's registration at a provider. RedirectURL is
// the exact registered callback URI; it is sent verbatim in the authorization
// and token requests and is never taken from an incoming request.
type Credentials struct {
	ClientID     string
	ClientSecret secret.String
	RedirectURL  string
}

// ProfileSource selects how a provider's user profile is read.
type ProfileSource uint8

const (
	// OpenIDConnect verifies the ID token returned by the token endpoint
	// (signature against JWKSURL, issuer, audience, expiry and nonce) and reads
	// its standard claims.
	OpenIDConnect ProfileSource = iota + 1
	// GitHubUser reads GitHub's REST user API with the access token, plus the
	// verified primary address from its emails API when that scope is granted.
	GitHubUser
)

// Provider is an immutable provider declaration. Use a preset such as Google
// or GitHub, or fill the fields for another OpenID Connect provider.
type Provider struct {
	Name             ProviderName
	Credentials      Credentials
	Source           ProfileSource
	AuthorizationURL string
	TokenURL         string
	Scopes           []string
	// Issuers and JWKSURL are required for OpenIDConnect.
	Issuers []string
	JWKSURL string
	// UserURL and EmailsURL are used by GitHubUser.
	UserURL   string
	EmailsURL string
}

// Google is the OpenID Connect preset (scopes openid, email, profile).
func Google(credentials Credentials) Provider {
	return Provider{Name: "google", Credentials: credentials, Source: OpenIDConnect,
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
		Scopes: []string{"openid", "email", "profile"}, Issuers: []string{"https://accounts.google.com", "accounts.google.com"},
		JWKSURL: "https://www.googleapis.com/oauth2/v3/certs"}
}

// GitHub is the OAuth 2.0 preset using GitHub's user API (scopes read:user,
// user:email). GitHub issues no ID token; the numeric account ID is the Subject.
func GitHub(credentials Credentials) Provider {
	return Provider{Name: "github", Credentials: credentials, Source: GitHubUser,
		AuthorizationURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
		Scopes: []string{"read:user", "user:email"}, UserURL: "https://api.github.com/user", EmailsURL: "https://api.github.com/user/emails"}
}

const maxScopes = 32

// Validate checks the declaration without I/O. Provider endpoints and the
// redirect URI must use HTTPS; plain HTTP is accepted only for loopback hosts
// (local development and tests).
func (p Provider) Validate() error {
	if !identifier.Semantic(string(p.Name)) || p.Credentials.ClientID == "" || len(p.Credentials.ClientID) > 512 || strings.ContainsFunc(p.Credentials.ClientID, controlOrSpace) || len(p.Credentials.ClientSecret.Reveal()) > 4096 {
		return fault.New(fault.Invalid, "OAuth provider requires a name and client ID")
	}
	redirect, err := endpoint(p.Credentials.RedirectURL)
	if err != nil {
		return err
	}
	for _, reserved := range []string{"code", "state", "error"} {
		if redirect.Query().Has(reserved) {
			return fault.New(fault.Invalid, "OAuth redirect URL must not carry OAuth callback parameters")
		}
	}
	required := []string{p.AuthorizationURL, p.TokenURL}
	switch p.Source {
	case OpenIDConnect:
		if len(p.Issuers) == 0 || len(p.Issuers) > 8 || !slices.Contains(p.Scopes, "openid") {
			return fault.New(fault.Invalid, "OpenID Connect providers require issuers and the openid scope")
		}
		for _, issuer := range p.Issuers {
			if issuer == "" || len(issuer) > 512 || strings.ContainsFunc(issuer, controlOrSpace) {
				return fault.New(fault.Invalid, "invalid OpenID Connect issuer")
			}
		}
		required = append(required, p.JWKSURL)
	case GitHubUser:
		required = append(required, p.UserURL)
		if p.EmailsURL != "" {
			required = append(required, p.EmailsURL)
		}
	default:
		return fault.New(fault.Invalid, "OAuth provider requires a profile source")
	}
	for _, address := range required {
		if _, err := endpoint(address); err != nil {
			return err
		}
	}
	if len(p.Scopes) > maxScopes {
		return fault.New(fault.Invalid, "too many OAuth scopes")
	}
	for _, scope := range p.Scopes {
		if scope == "" || len(scope) > 256 || strings.ContainsFunc(scope, controlOrSpace) {
			return fault.New(fault.Invalid, "invalid OAuth scope")
		}
	}
	return nil
}

// snapshot copies caller-owned slices.
func (p Provider) snapshot() Provider {
	p.Scopes = slices.Clone(p.Scopes)
	p.Issuers = slices.Clone(p.Issuers)
	return p
}

// endpoint accepts an absolute HTTPS URL, or HTTP for a loopback host, with no
// user information or fragment.
func endpoint(text string) (*url.URL, error) {
	invalid := fault.New(fault.Invalid, "OAuth endpoints must be absolute HTTPS URLs")
	if text == "" || len(text) > 2048 || strings.ContainsFunc(text, controlOrSpace) {
		return nil, invalid
	}
	parsed, err := url.Parse(text)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, invalid
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !loopback(parsed.Hostname()) {
			return nil, invalid
		}
	default:
		return nil, invalid
	}
	return parsed, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

func controlOrSpace(r rune) bool { return r <= ' ' || r == 0x7f }

// Destinations returns an SSRF-restricted outbound policy admitting only this
// provider's HTTPS hosts on port 443, for the httpclient.Client passed to NewClient.
func (p Provider) Destinations() (httpclient.DestinationPolicy, error) {
	if err := p.Validate(); err != nil {
		return httpclient.DestinationPolicy{}, err
	}
	policy := httpclient.PublicDestinations()
	for _, address := range []string{p.TokenURL, p.JWKSURL, p.UserURL, p.EmailsURL} {
		if address == "" {
			continue
		}
		parsed, err := endpoint(address)
		if err != nil {
			return httpclient.DestinationPolicy{}, err
		}
		if !slices.Contains(policy.Hosts, parsed.Hostname()) {
			policy.Hosts = append(policy.Hosts, parsed.Hostname())
		}
	}
	return policy, policy.Validate()
}
