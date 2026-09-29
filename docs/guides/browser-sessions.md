# Browser sessions and CSRF

`http.BrowserSessions[M,K]` connects a [persistent model session](sessions.md) to
ordinary typed HTTP endpoints. It owns cookie parsing, CSRF policy, credential
changes and cookie publication. Handlers keep returning their declared DTOs.
This HTTP slice passed focused races and full native regression in 503.3 seconds,
including 721 compiler cases, 271 editor probes and current generated output.
Password verification and MFA challenge flows remain separate milestone 10 work.

## Assemble once

```go
registry, err := auth.NewRegistry(auth.DefaultConfig(), sessions.Guard().Registration())
// Check err before continuing.
web, err := http.NewBrowserSessions(registry, sessions, http.DefaultBrowserSessionConfig())
// Check err before registering routes.
```

`sessions` is the existing `*session.Sessions[User, model.ID[User]]` (or the model's
natural-key type). The [compiling consumer](../../tests/fixtures/consumer/authenticating/browser.go)
shows login, profile, rotation and logout with public framework imports only.

Defaults use `__Host-foundry_session`, host-only scope at `/`, Secure, HttpOnly and
SameSite=Lax. Secure sessions reject insecure requests. Run `TrustedProxy` first
when a trusted edge supplies the public HTTPS scheme; arbitrary forwarding headers
never make a request secure. For explicit local HTTP development, use a cookie name
without a secure prefix and set `Options.Secure=false`. Cookie policy is immutable
after construction. Custom paths/domains must satisfy the chosen cookie prefix.

The HTTP clock must agree with the persistence adapter's clock. Both default to the
system clock; deterministic tests inject the same `testkit.Clock`. Server session
policy owns expiry. Browser configuration cannot override MaxAge or Expires.

## Typed handlers

Public login/logout endpoints add `web.Middleware()` using ordinary
`Endpoint.WithMiddleware`. Guarded endpoints use
`http.RequireAuthentication(endpoint, web.Authentication(), web.Guard())`; that
transport automatically establishes browser-session policy before authentication.
A scope-level browser middleware and its matching authenticated routes safely reuse
one request binding. A different browser adapter cannot reuse that binding.

Inside a public typed login handler, after trusted credential verification:

```go
info, err := web.Login(ctx, verifiedProof, session.IssueOptions{Remember: true})
// Check err, then build your ordinary response DTO from allowed presentation values.
```

`verifiedProof` must match the concrete model and stored key type. Calling
`auth.NewProof` on a submitted ID is not credential verification. The consumer's
verifier callback is an acceptance fixture, not a production password login flow.

Other handlers use:

```go
info, err := web.Rotate(ctx)
err = web.Logout(ctx)
```

These are alternative operations, not a sequence in one handler. Only one
credential-changing attempt is allowed per request. Use an unsafe method such as
POST. GET/HEAD/OPTIONS cannot log in, rotate or log out. Operations require an
ordinary typed JSON or empty response; raw/download/stream handlers cannot publish
a login through these helpers. Configure normal transport middleware to preserve
the typed endpoint's ownership of its response.

`Login` issues a fresh credential and then revokes the supplied previous cookie
before staging its replacement. Issuance counts against the active-session limit:
with the default `auth.EvictOldest` policy the subject's oldest session is revoked
when the cap is reached, and with `auth.RejectNew` the login fails with
`auth.CredentialLimit` (HTTP 409). Pending-MFA sessions have their own cap. `Rotate` invalidates the old
secret while preserving the session identity and absolute deadline. `Logout`
revokes the credential before staging an exact-scope cookie removal. Missing
credentials may still be logged out; malformed cookie input is rejected earlier.

Sensitive routes can require a recent password confirmation on the current
session. A confirmation route re-verifies the password with
`login.Confirm(ctx, user, password)` and then calls `sessions.ConfirmCurrent(ctx)`.
The confirmation route must be throttled, or a stolen session cookie can
brute-force the password there: configure the login with
`login.WithConfirmationLockout(throttle)`, a per-subject declaration such as
`lockout.Define("accounts.confirm", auth.ConfirmationKeys(), lockout.DefaultPolicy())`
(a locked subject gets 429 before any hashing), and add an HTTP rate limit.
Protected routes add the actor-stage middleware:

```go
route := http.RequireAuthentication(endpoint, browser.Authentication(), browser.Guard()).
    WithActorMiddleware(browser.RequirePasswordConfirmation(15 * time.Minute))
```

Without a confirmation within the window, checked against the session store's
clock, the request fails with `auth.ConfirmationRequired` (HTTP 403 `forbidden`)
before decoding. See [sessions](sessions.md#the-current-session).

Impersonation within one browser guard uses the session binding's
[impersonation](sessions.md#impersonation) and swaps the cookie:
`browser.Impersonate(ctx, support, target, duration)` stages the impersonation
session cookie without revoking the actor's session, and
`browser.StopImpersonating(ctx, support)` ends it and stages the actor's own
session again under a fresh secret, with its original expiry (clearing the cookie
when the actor's session has ended). Add
`WithActorMiddleware(browser.RefuseImpersonation())` to routes that change
credentials, email, MFA factors or the account, or that mint tokens; they then
reject impersonation sessions with 403 before decoding (minting through
`CurrentProof` is refused regardless). Impersonation cookies are never persistent.

A regular session uses a nonpersistent browser cookie. Remembered cookies expire
at the session's absolute server deadline. Sliding idle expiry remains enforced
on the server and never extends the browser cookie beyond that absolute deadline.
Pending-MFA sessions cannot be remembered or become fully authenticated by rotation.

## Cookie publication and failure

Only a successful handler and successfully prepared response can publish the staged
cookie. Handler errors, invalid DTO encoding, panic, Goexit, cancellation and expiry
before publication withhold it. Only a credential-bearing publication (a staged
login, rotation or logout cookie) is withheld once the request context ended; an
ordinary response completed after its deadline is still delivered. Session work
ends on client disconnect or forced shutdown, while the handler's own context
carries its route's deadline, so a route with `WithTimeout` can log in after the
kernel `RequestTimeout`. The native writer is preserved; no response body is
buffered by this integration. Public handlers returning auth errors use the same
HTTP 401/403/MFA contracts as guarded handlers. Explicit domain HTTP errors retain
their declared status and message even when they wrap an auth error.

A request owns all admitted credential operations until they actually return.
Retained contexts cannot mutate a completed response, including through
`context.WithoutCancel`. A response racing an unfinished credential change fails;
late results cannot publish a cookie. Browser routes default to `Cache-Control:
no-store`, and credential material does not become a DTO or route URL.

Database persistence and HTTP delivery are separate. A failed response, lost network
acknowledgement, or failed revocation after new issuance can leave a session that
was never delivered. The framework does not retry these mutations or claim atomic
cookie delivery. Such sessions remain bounded by server policy and can be expired,
pruned or explicitly revoked. A failure after old-session revocation can require
the browser to authenticate again.

## CSRF policy

Browser routes protect pre-authentication login as well as authenticated mutations.
The default uses Go's maintained
[CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection), with
bounded, nonduplicate headers and a scheme-aware Origin fallback. Unsafe requests
with neither Fetch Metadata nor Origin evidence are rejected. This chooses explicit
rejection for clients that cannot provide origin evidence, rather than copying the
Rust cookie/header token protocol. Go's built-in protection is also discussed in
[OWASP's CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html).

Same-origin browser requests work without manually handling a CSRF token. A
cross-origin or sibling-site mutation is rejected unless its exact origin appears
in `BrowserSessionConfig.CSRF.TrustedOrigins`. Null/wildcard origins do not grant
trust. When falling back to Origin, scheme, host and port must match the validated
public request origin or an explicit trusted origin. Repeated, malformed or
contradictory evidence fails before the handler. GET/HEAD/OPTIONS remain available
for safe navigation and preflight; application code must keep them free of domain
state changes.

CORS controls response sharing and does not imply CSRF trust. Configure it separately
for deliberate cross-origin clients. Native/bearer API routes should use their own
authentication transport, rather than weakening browser-session checks. Standalone
`http.CSRF(http.CSRFConfig{...})` supplies the same origin policy for other browser
routes and preserves the native writer. Responses to unsafe methods vary on
`Sec-Fetch-Site` and `Origin`; cacheable safe-method responses do not, because
their decision never depends on those fields. The check reads only those two
fields, without copying other request headers. No token/header compatibility
with Rust or Laravel is implied.

## Complete a pending MFA cookie

`BrowserSessions.CompleteMFA` passed milestone 10 acceptance. On a public typed
challenge endpoint protected by `browser.Middleware()`, pass the model-bound
second-factor verifier and server-selected session options. It consumes the bound
pending cookie and stages a fresh authenticated cookie using the same CSRF,
request ownership and publication checks as login/rotation. See the
[MFA guide](mfa.md#http-delivery) and its consumer routes. A response failure after
database commit may leave the challenge consumed; restart login rather than
replaying completion.
