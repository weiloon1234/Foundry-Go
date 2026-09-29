# Social login with OAuth 2.0 and OpenID Connect

`auth/oauth` is an authorization-code client for "Sign in with Google/GitHub"
style login. It returns a typed `oauth.Profile`; linking it to an existing user
or creating one, and then issuing a session or token, is the application's
decision. It uses only the standard library cryptography.

## Providers

```go
google := oauth.Google(oauth.Credentials{
    ClientID:     settings.GoogleClientID,
    ClientSecret: settings.GoogleClientSecret, // secret.String from a secret file
    RedirectURL:  "https://app.example/auth/google/callback",
})
github := oauth.GitHub(oauth.Credentials{ /* ... */ })
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

```go
policy, err := google.Destinations()
config := httpclient.DefaultConfig("oauth.google")
config.Destination = policy
outbound, err := httpclient.New(config, nil)
client, err := oauth.NewClient(google, outbound, clock.System{})
```

`NewClient` rejects an unrestricted client.

## Redirect and callback

`oauth.Flow` keeps the pending request (state, nonce and PKCE verifier) in an
encrypted, short-lived cookie, so no server-side store is needed. It uses the
[application key ring](encryption.md#application-key-ring) through
`Services.CookieEncrypter()`:

```go
encrypter, err := services.CookieEncrypter()
flow, err := oauth.NewFlow(client, encrypter, "__Host-foundry_oauth_google")

redirect := http.DefineRoute(http.RouteSpec{ID: "auth.google", Method: http.GET, Access: http.Public}, http.StaticPath("/auth/google")).
    HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
        location, err := flow.Start(r.Context(), w)
        if err != nil {
            _ = http.WriteError(w, r, err)
            return
        }
        stdhttp.Redirect(w, r, location, stdhttp.StatusFound)
    })
callback := http.DefineRoute(http.RouteSpec{ID: "auth.google.callback", Method: http.GET, Access: http.Public}, http.StaticPath("/auth/google/callback")).
    HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
        profile, err := flow.Finish(w, r)
        if err != nil {
            _ = http.WriteError(w, r, err)
            return
        }
        // Find or create the user by (profile.Provider, profile.Subject), then
        // issue a session, for example browser.Login(ctx, proof, options).
    })
```

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
