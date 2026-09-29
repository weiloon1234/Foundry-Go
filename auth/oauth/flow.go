package oauth

import (
	"context"
	stdhttp "net/http"
	"net/url"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
)

// Flow keeps the pending request in an encrypted, short-lived cookie between
// the redirect and the callback, so no server-side state is needed. The cookie
// is authenticated and scoped by the application key ring (http.CookieEncrypter),
// HttpOnly, Secure and SameSite=Lax (sent on the provider's top-level redirect
// back), and expires with the pending request.
type Flow struct {
	client   *Client
	cookie   http.EncryptedCookie[Pending]
	callback string
}

// NewFlow binds a client to a pending-request cookie. Use one cookie name per
// provider, for example "__Host-foundry_oauth_google".
func NewFlow(client *Client, encrypter http.CookieEncrypter, name http.CookieName) (*Flow, error) {
	if err := client.Validate(); err != nil {
		return nil, err
	}
	options := http.DefaultCookieOptions()
	options.MaxAge = client.lifetime
	cookie := http.DefineCookie(name, http.CookieCodec[Pending](pendingCodec{}), options).Encrypted(encrypter)
	if err := cookie.Validate(); err != nil {
		return nil, err
	}
	redirect, err := endpoint(client.provider.Credentials.RedirectURL)
	if err != nil {
		return nil, err
	}
	return &Flow{client: client, cookie: cookie, callback: redirect.EscapedPath()}, nil
}

// Start begins an authorization request, stores its pending half in the cookie
// and returns the provider URL. Respond with a redirect to it (for example
// http.Redirect with 302) or return it to a single-page client.
func (f *Flow) Start(ctx context.Context, w stdhttp.ResponseWriter) (string, error) {
	if f == nil {
		return "", fault.New(fault.Invalid, "OAuth flow is not configured")
	}
	authorization, err := f.client.Begin(ctx)
	if err != nil {
		return "", err
	}
	if err := f.cookie.Set(ctx, w, authorization.Pending); err != nil {
		return "", err
	}
	return authorization.URL, nil
}

// Finish handles the provider's redirect to the registered callback: it reads
// and always clears the pending cookie (it is single use), validates the query
// and completes the flow. A missing, expired or tampered cookie, a callback on
// another path, a state mismatch or a provider denial return
// auth.Unauthenticated.
func (f *Flow) Finish(w stdhttp.ResponseWriter, r *stdhttp.Request) (Profile, error) {
	if f == nil || r == nil {
		return Profile{}, fault.New(fault.Invalid, "OAuth flow is not configured")
	}
	stored, err := f.cookie.Read(r)
	if clearErr := f.cookie.Clear(w); clearErr != nil {
		return Profile{}, clearErr
	}
	if err != nil {
		return Profile{}, auth.Unauthenticated.WithCause(err)
	}
	pending, present := stored.Get()
	if !present {
		return Profile{}, auth.Unauthenticated.WithCause(fault.New(fault.Missing, "OAuth callback has no pending request"))
	}
	if r.URL.EscapedPath() != f.callback {
		return Profile{}, auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "OAuth callback arrived on an unregistered path"))
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return Profile{}, auth.Unauthenticated.WithCause(fault.New(fault.Invalid, "malformed OAuth callback"))
	}
	callback, err := ParseCallback(query)
	if err != nil {
		return Profile{}, err
	}
	return f.client.Complete(r.Context(), pending, callback)
}
