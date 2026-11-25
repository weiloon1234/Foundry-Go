# Social login with OAuth 2.0 and OpenID Connect

`auth/oauth` is an authorization-code client for "Sign in with Google/GitHub"
style login. It returns a typed `oauth.Profile`; linking it to an existing user
or creating one, and then issuing a session or token, is the application's
decision. It uses only the standard library cryptography.

## Providers

The application owns `models.User` and any generated tables or identities used below.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `application`, `auth`, `clock`, `http`, `httpclient`, `model`, `auth/oauth`, `secret`, `auth/session`. Standard-library aliases: `net/http` as `stdhttp`.

Inputs: `googleID string`, `googleSecret secret.String`, `githubID string`, `githubSecret secret.String`.

<!-- compiled-source: tests/fixtures/consumer/authenticating/oauth_recipes.go#social-providers -->
```go
google := oauth.Google(oauth.Credentials{
	ClientID:     googleID,
	ClientSecret: googleSecret, // Loaded through the application's secret configuration.
	RedirectURL:  "https://app.example/auth/google/callback",
})
github := oauth.GitHub(oauth.Credentials{
	ClientID:     githubID,
	ClientSecret: githubSecret,
	RedirectURL:  "https://app.example/auth/github/callback",
})
```

`Google` is an OpenID Connect provider (scopes `openid email profile`): its ID
token is verified against Google's JWKS. `GitHub` is OAuth 2.0 with GitHub's user
API (scopes `read:user user:email`). Another OpenID Connect provider fills
`oauth.Provider` directly: `Source: oauth.OpenIDConnect`, the authorization,
token and JWKS URLs, its accepted `Issuers` and the `openid` scope.
`Provider.Validate` requires HTTPS endpoints and redirect URI (plain HTTP only for
loopback hosts); the redirect URI must be the exact value registered at the
provider and cannot carry `code`, `state` or `error` parameters.

Provider requests go through a destination-restricted `httpclient.Client`, so a
provider URL cannot reach internal services. `provider.Destinations()` returns a
policy admitting only that provider's hosts on port 443:

Inputs: `ctx context.Context`, `google oauth.Provider`.

<!-- compiled-source: tests/fixtures/consumer/authenticating/oauth_recipes.go#social-client -->
```go
policy, err := google.Destinations()
if err != nil {
	return nil, nil, err
}
config := httpclient.DefaultConfig("oauth.google")
config.Destination = policy
outbound, err := httpclient.New(config, nil)
if err != nil {
	return nil, nil, err
}
client, err := oauth.NewClient(google, outbound, clock.System{})
if err != nil {
	return nil, nil, errors.Join(err, outbound.Close(ctx))
}
// The application owns outbound and closes it during shutdown.
```

`NewClient` rejects an unrestricted client. The application owns the returned
outbound client and closes it during shutdown.

## Redirect and callback

`oauth.Flow` keeps the pending request (state, nonce and PKCE verifier) in an
encrypted, short-lived cookie, so the provider round trip needs no server-side
pending-request store. It uses the
[application key ring](encryption.md#application-key-ring) through
`Services.CookieEncrypter()`:

The application must supply `SocialHandoffs`: a bounded, expiring, atomic single-use store that resolves the verified provider identity to its current local account and assurance policy.

<!-- compiled-source: tests/fixtures/consumer/authenticating/oauth_recipes.go#social-handoff-state -->
```go
// SocialHandoffs is application-owned, bounded, expiring, single-use login state.
// Stage returns an unguessable token and stores only the verified identity needed
// for login. Consume atomically removes it, resolves the current local account
// and returns its proof with the application's required assurance.
type SocialHandoffs interface {
	Stage(context.Context, oauth.Profile) (secret.String, error)
	Consume(context.Context, secret.String) (auth.Proof[models.User, model.ID[models.User]], error)
}

type socialHandoffContextKey struct{}
```

Inputs: `services application.Services`, `client *oauth.Client`, `web *http.BrowserSessions[models.User, model.ID[models.User]]`, `handoffs SocialHandoffs`.

<!-- compiled-source: tests/fixtures/consumer/authenticating/oauth_recipes.go#social-flow-routes -->
```go
encrypter, err := services.CookieEncrypter()
if err != nil {
	return nil, err
}
flow, err := oauth.NewFlow(client, encrypter, "__Host-foundry_oauth_google")
if err != nil {
	return nil, err
}
cookie := http.DefineCookie("__Host-social-handoff", http.SecretCookie(), http.DefaultCookieOptions())

startRoute := http.DefineRoute(http.RouteSpec{ID: "auth.google", Method: http.GET, Access: http.Public}, http.StaticPath("/auth/google"))
start := http.HandleRaw(startRoute, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
	location, err := flow.Start(r.Context(), w)
	if err != nil {
		http.WriteError(w, r, err)
		return
	}
	stdhttp.Redirect(w, r, location, stdhttp.StatusFound)
})
callbackRoute := http.DefineRoute(http.RouteSpec{ID: "auth.google.callback", Method: http.GET, Access: http.Public}, http.StaticPath("/auth/google/callback"))
callback := http.HandleRaw(callbackRoute, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
	profile, err := flow.Finish(w, r)
	if err != nil {
		http.WriteError(w, r, err)
		return
	}
	handoff, err := handoffs.Stage(r.Context(), profile)
	if err != nil {
		http.WriteError(w, r, err)
		return
	}
	if err := cookie.Set(r.Context(), w, handoff); err != nil {
		http.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	io.WriteString(w, `<form method="post" action="/auth/google/complete"><button>Continue signing in</button></form>`)
})
capture := http.DefineMiddleware("auth.google.handoff", func(next stdhttp.Handler) (stdhttp.Handler, error) {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		supplied, err := cookie.Read(r)
		if err != nil {
			http.WriteError(w, r, err)
			return
		}
		handoff, present := supplied.Get()
		if !present {
			http.WriteError(w, r, auth.Unauthenticated)
			return
		}
		if err := cookie.Clear(w); err != nil {
			http.WriteError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), socialHandoffContextKey{}, handoff)
		next.ServeHTTP(w, r.WithContext(ctx))
	}), nil
})
completeRoute := http.DefineRoute(http.RouteSpec{ID: "auth.google.complete", Method: http.POST, Access: http.Public}, http.StaticPath("/auth/google/complete"))
endpoint := http.DefineEndpoint(completeRoute, http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
complete := http.ConfigureEndpoint(endpoint, http.Use(web.Middleware(), capture)).Handle(func(ctx context.Context, _ http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.NoContent, error) {
	handoff, ok := ctx.Value(socialHandoffContextKey{}).(secret.String)
	if !ok {
		return http.NoContent{}, auth.Unauthenticated
	}
	proof, err := handoffs.Consume(ctx, handoff)
	if err != nil {
		return http.NoContent{}, err
	}
	_, err = web.Login(ctx, proof, session.IssueOptions{})
	return http.NoContent{}, err
})
router, err := http.NewRouter(start, callback, complete)
```

The callback writes only an opaque handoff to a Secure, HttpOnly cookie and shows a
same-origin confirmation form. Browser middleware protects the POST against CSRF;
its typed handler consumes the handoff before calling `web.Login`. The GET callback
never mutates the browser session. This is transport wiring, not a complete login
application: the handoff store and account-linking policy remain application code.
A failed POST can consume the handoff without delivering a session, so restart
login rather than retrying it automatically.

`Start` creates 256-bit state, nonce and PKCE verifier and returns the provider
URL with `response_type=code`, the exact redirect URI and the S256 code challenge
(and the nonce for OpenID Connect). The cookie is HttpOnly, Secure,
`SameSite=Lax` (sent on the provider's redirect back) and expires after
`oauth.DefaultPendingLifetime` (10 minutes). `Finish` reads and always clears
the cookie, requires the callback on the registered path, compares the state in
constant time, exchanges the code with the verifier and the exact redirect URI,
and reads the profile. Starting again in another tab replaces the pending
request, so the older callback is rejected.

Applications that keep state server-side use `client.Begin(ctx)`, store
`Authorization.Pending` (it redacts itself), and later call
`client.Complete(ctx, pending, callback)` with `oauth.ParseCallback(query)`. A
pending request is single use.

## Verification and failures

For OpenID Connect the ID token must be a compact JWS signed with RS256 (RSA of
at least 2048 bits) or ES256 (P-256) by a key from the provider's JWKS; `none`,
HMAC and critical header extensions are rejected. The issuer must be one of
`Issuers`, the audience must contain the client ID (with `azp` equal to it when
there are several audiences or `azp` is present), `exp` and `iat` are checked with
one minute of skew, the nonce must match the pending request and `sub` must be
present. Keys are cached for an hour; an unknown key ID refetches the JWKS at
most once a minute, so rotated keys are picked up without letting forged key IDs
cause a provider request per callback.

Every rejected callback (missing, expired or tampered pending cookie, state
mismatch, a provider `error` such as `access_denied` (cause `oauth.Denied`), a
rejected code or an invalid ID token) returns `auth.Unauthenticated`. Network and
provider failures remain operational errors.

## Profiles

`Profile.Subject` is the stable account identifier at the provider (the `sub`
claim, or GitHub's numeric account ID) and is what to link on; store it with
`Profile.Provider`. `Email` is trustworthy only when `EmailVerified`: Google's
`email_verified` claim, or the verified primary address from GitHub's emails API
(the public profile email is never treated as verified). Do not link an existing
account by an unverified email. `Profile.Token` holds the provider access token
(and refresh token and expiry when issued) for further API calls; it redacts
itself in formatting, logs and JSON.
