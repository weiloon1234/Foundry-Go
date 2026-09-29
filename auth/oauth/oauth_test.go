package oauth_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/oauth"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

const clientID = "client-123"

func segment(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

// signer produces compact JWS tokens for the fake provider.
type signer struct {
	kid string
	rsa *rsa.PrivateKey
	ec  *ecdsa.PrivateKey
}

func (s signer) alg() string {
	if s.ec != nil {
		return "ES256"
	}
	return "RS256"
}

func (s signer) jwk() map[string]string {
	if s.ec != nil {
		point, _ := s.ec.PublicKey.Bytes()
		return map[string]string{"kty": "EC", "kid": s.kid, "use": "sig", "crv": "P-256", "x": segment(point[1:33]), "y": segment(point[33:])}
	}
	return map[string]string{"kty": "RSA", "kid": s.kid, "use": "sig", "alg": "RS256", "n": segment(s.rsa.N.Bytes()), "e": segment(big.NewInt(int64(s.rsa.E)).Bytes())}
}

func (s signer) sign(t *testing.T, header map[string]any, claims map[string]any) string {
	t.Helper()
	if header == nil {
		header = map[string]any{"alg": s.alg(), "kid": s.kid, "typ": "JWT"}
	}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	input := segment(headerJSON) + "." + segment(claimsJSON)
	digest := sha256.Sum256([]byte(input))
	var signature []byte
	var err error
	if s.ec != nil {
		r, sv, signErr := ecdsa.Sign(rand.Reader, s.ec, digest[:])
		err = signErr
		signature = append(r.FillBytes(make([]byte, 32)), sv.FillBytes(make([]byte, 32))...)
	} else {
		signature, err = rsa.SignPKCS1v15(rand.Reader, s.rsa, crypto.SHA256, digest[:])
	}
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + segment(signature)
}

// fakeProvider is an httptest OpenID Connect and GitHub-style provider.
type fakeProvider struct {
	server     *httptest.Server
	mu         sync.Mutex
	keys       []signer
	jwksCalls  atomic.Int32
	challenges map[string]string // code -> PKCE challenge
	idToken    func(nonce string) string
	nonces     map[string]string // code -> nonce
	emails     int               // status for /user/emails
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	f := &fakeProvider{challenges: map[string]string{}, nonces: map[string]string{}, emails: 200}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeProvider) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/jwks":
		f.jwksCalls.Add(1)
		f.mu.Lock()
		keys := make([]map[string]string, len(f.keys))
		for i, key := range f.keys {
			keys[i] = key.jwk()
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	case "/token":
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("client_id") != clientID || r.PostForm.Get("client_secret") != "client-secret" || r.PostForm.Get("redirect_uri") != "https://app.test/auth/callback" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
			return
		}
		code := r.PostForm.Get("code")
		f.mu.Lock()
		challenge, known := f.challenges[code]
		nonce := f.nonces[code]
		delete(f.challenges, code)
		f.mu.Unlock()
		verifier := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !known || segment(verifier[:]) != challenge {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		body := map[string]any{"access_token": "access-" + code, "token_type": "Bearer", "expires_in": 3600, "scope": "read:user,user:email"}
		if f.idToken != nil {
			body["id_token"] = f.idToken(nonce)
		}
		_ = json.NewEncoder(w).Encode(body)
	case "/user":
		if r.Header.Get("Authorization") != "Bearer access-good" || r.Header.Get("User-Agent") == "" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"id":12345,"login":"octo","name":null,"email":"public@example.test","avatar_url":"https://avatars.example.test/1"}`))
	case "/user/emails":
		if f.emails != 200 {
			w.WriteHeader(f.emails)
			return
		}
		_, _ = w.Write([]byte(`[{"email":"other@example.test","primary":false,"verified":true},{"email":"octo@example.test","primary":true,"verified":true}]`))
	default:
		w.WriteHeader(404)
	}
}

// authorize simulates the provider's consent step for a Begin URL.
func (f *fakeProvider) authorize(t *testing.T, location string, code string) url.Values {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("response_type") != "code" || query.Get("client_id") != clientID || query.Get("redirect_uri") != "https://app.test/auth/callback" || query.Get("code_challenge_method") != "S256" || len(query.Get("state")) != 43 {
		t.Fatal("authorization request is incomplete", query)
	}
	f.mu.Lock()
	f.challenges[code] = query.Get("code_challenge")
	f.nonces[code] = query.Get("nonce")
	f.mu.Unlock()
	return url.Values{"code": {code}, "state": {query.Get("state")}}
}

func outbound(t *testing.T, server *httptest.Server) *httpclient.Client {
	t.Helper()
	address, err := netip.ParseAddrPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	config := httpclient.DefaultConfig("oauth.test")
	config.Retry = httpclient.NoRetries()
	config.Destination = httpclient.DestinationPolicy{Mode: httpclient.RestrictedDestinations, Schemes: []httpclient.Scheme{httpclient.HTTP}, Ports: []uint16{address.Port()}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
	client, err := httpclient.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(t.Context()) })
	return client
}

func credentials() oauth.Credentials {
	return oauth.Credentials{ClientID: clientID, ClientSecret: secret.New("client-secret"), RedirectURL: "https://app.test/auth/callback"}
}

func googleLike(f *fakeProvider) oauth.Provider {
	p := oauth.Google(credentials())
	p.AuthorizationURL = "https://provider.test/authorize"
	p.TokenURL, p.JWKSURL = f.server.URL+"/token", f.server.URL+"/jwks"
	p.Issuers = []string{"https://provider.test"}
	return p
}

func claimsAt(now time.Time, nonce string) map[string]any {
	return map[string]any{"iss": "https://provider.test", "sub": "user-42", "aud": clientID, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": nonce, "email": "person@example.test", "email_verified": true, "name": "Person"}
}

func rsaSigner(t *testing.T, kid string) signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return signer{kid: kid, rsa: key}
}

// The authorization-code flow sends PKCE S256, state and nonce, exchanges the
// code with the verifier and verifies the RS256 ID token into a typed profile.
func TestOpenIDConnectFlowVerifiesIDToken(t *testing.T) {
	f := newFakeProvider(t)
	key := rsaSigner(t, "rsa1")
	f.keys = []signer{key}
	now := testkit.NewClock(time.Now())
	f.idToken = func(nonce string) string { return key.sign(t, nil, claimsAt(now.Now(), nonce)) }
	client, err := oauth.NewClient(googleLike(f), outbound(t, f.server), now)
	if err != nil {
		t.Fatal(err)
	}
	started, err := client.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(started.URL)
	if state := parsed.Query().Get("state"); strings.Contains(fmt.Sprintf("%v %+v", started, started.Pending), state) {
		t.Fatal("authorization formatting leaked the state")
	}
	callback, err := oauth.ParseCallback(f.authorize(t, started.URL, "good"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.Complete(t.Context(), started.Pending, callback)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Provider != "google" || profile.Subject != "user-42" || profile.Email != "person@example.test" || !profile.EmailVerified || profile.Name != "Person" || profile.Token.Access.Reveal() != "access-good" {
		t.Fatal("unexpected profile", profile.Subject, profile.Email)
	}
	if expires, ok := profile.Token.ExpiresAt.Get(); !ok || !expires.Equal(now.Now().Add(time.Hour).UTC()) {
		t.Fatal("token expiry lost")
	}
	if strings.Contains(fmt.Sprintf("%v %+v", profile.Token, started.Pending), "access-good") {
		t.Fatal("token formatting leaked the credential")
	}
	// A replayed code (the pending request is single use) is rejected.
	if _, err := client.Complete(t.Context(), started.Pending, callback); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("replayed authorization code accepted", err)
	}
}

// Every mismatch between callback, pending request and ID token is rejected.
func TestOpenIDConnectRejectsInvalidCallbacksAndTokens(t *testing.T) {
	f := newFakeProvider(t)
	key, other := rsaSigner(t, "rsa1"), rsaSigner(t, "rsa1")
	f.keys = []signer{key}
	now := testkit.NewClock(time.Now())
	client, err := oauth.NewClient(googleLike(f), outbound(t, f.server), now)
	if err != nil {
		t.Fatal(err)
	}
	hmacToken := func(nonce string) string {
		header, _ := json.Marshal(map[string]any{"alg": "HS256", "kid": "rsa1"})
		claims, _ := json.Marshal(claimsAt(now.Now(), nonce))
		input := segment(header) + "." + segment(claims)
		mac := hmac.New(sha256.New, []byte("client-secret"))
		mac.Write([]byte(input))
		return input + "." + segment(mac.Sum(nil))
	}
	cases := map[string]func(nonce string) string{
		"nonce": func(string) string { return key.sign(t, nil, claimsAt(now.Now(), "another-nonce")) },
		"audience": func(n string) string {
			c := claimsAt(now.Now(), n)
			c["aud"] = "someone-else"
			return key.sign(t, nil, c)
		},
		"azp": func(n string) string {
			c := claimsAt(now.Now(), n)
			c["aud"] = []string{clientID, "other"}
			return key.sign(t, nil, c)
		},
		"issuer": func(n string) string {
			c := claimsAt(now.Now(), n)
			c["iss"] = "https://evil.test"
			return key.sign(t, nil, c)
		},
		"expired": func(n string) string {
			c := claimsAt(now.Now(), n)
			c["exp"] = now.Now().Add(-2 * time.Minute).Unix()
			return key.sign(t, nil, c)
		},
		"future": func(n string) string {
			c := claimsAt(now.Now(), n)
			c["iat"] = now.Now().Add(time.Hour).Unix()
			return key.sign(t, nil, c)
		},
		"subject":   func(n string) string { c := claimsAt(now.Now(), n); delete(c, "sub"); return key.sign(t, nil, c) },
		"signature": func(n string) string { return other.sign(t, nil, claimsAt(now.Now(), n)) },
		"none":      func(n string) string { return key.sign(t, map[string]any{"alg": "none"}, claimsAt(now.Now(), n)) },
		"hmac":      hmacToken,
		"critical": func(n string) string {
			return key.sign(t, map[string]any{"alg": "RS256", "kid": "rsa1", "crit": []string{"x"}}, claimsAt(now.Now(), n))
		},
	}
	for name, token := range cases {
		f.idToken = token
		started, err := client.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		callback, _ := oauth.ParseCallback(f.authorize(t, started.URL, "code-"+name))
		if _, err := client.Complete(t.Context(), started.Pending, callback); !errors.Is(err, auth.Unauthenticated) {
			t.Fatal("invalid ID token accepted:", name, err)
		}
	}
	f.idToken = func(nonce string) string { return key.sign(t, nil, claimsAt(now.Now(), nonce)) }
	started, _ := client.Begin(t.Context())
	good := f.authorize(t, started.URL, "good")
	for name, query := range map[string]url.Values{
		"state":  {"code": {"good"}, "state": {strings.Repeat("A", 43)}},
		"denied": {"error": {"access_denied"}, "state": good["state"]},
		"code":   {"code": {"unknown"}, "state": good["state"]},
	} {
		callback, err := oauth.ParseCallback(query)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Complete(t.Context(), started.Pending, callback)
		if !errors.Is(err, auth.Unauthenticated) || name == "denied" && !errors.Is(err, oauth.Denied) {
			t.Fatal("invalid callback accepted:", name, err)
		}
	}
	if _, err := oauth.ParseCallback(url.Values{"code": {"a", "b"}}); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("repeated callback parameter accepted", err)
	}
	now.Advance(oauth.DefaultPendingLifetime)
	callback, _ := oauth.ParseCallback(good)
	if _, err := client.Complete(t.Context(), started.Pending, callback); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("expired pending request accepted", err)
	}
}

// ES256 keys verify, and a rotated key is fetched at most once per refresh
// floor, so forged key IDs cannot force a provider request per callback.
func TestOpenIDConnectKeyRotationAndES256(t *testing.T) {
	f := newFakeProvider(t)
	old := rsaSigner(t, "rsa1")
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rotated := signer{kid: "ec1", ec: ecKey}
	f.keys = []signer{old}
	now := testkit.NewClock(time.Now())
	client, err := oauth.NewClient(googleLike(f), outbound(t, f.server), now)
	if err != nil {
		t.Fatal(err)
	}
	complete := func(key signer, code string) error {
		f.idToken = func(nonce string) string { return key.sign(t, nil, claimsAt(now.Now(), nonce)) }
		started, err := client.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		callback, _ := oauth.ParseCallback(f.authorize(t, started.URL, code))
		_, err = client.Complete(t.Context(), started.Pending, callback)
		return err
	}
	if err := complete(old, "c1"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.keys = []signer{old, rotated}
	f.mu.Unlock()
	if err := complete(rotated, "c2"); !errors.Is(err, auth.Unauthenticated) || f.jwksCalls.Load() != 1 {
		t.Fatal("unknown key refetched within the refresh floor", err, f.jwksCalls.Load())
	}
	now.Advance(2 * time.Minute)
	if err := complete(rotated, "c3"); err != nil || f.jwksCalls.Load() != 2 {
		t.Fatal("rotated ES256 key was not fetched", err, f.jwksCalls.Load())
	}
	if err := complete(old, "c4"); err != nil || f.jwksCalls.Load() != 2 {
		t.Fatal("cached keys were not reused", err)
	}
}

// GitHub has no ID token: the numeric account ID is the subject and only the
// verified primary address from the emails API is marked verified.
func TestGitHubProfileUsesVerifiedPrimaryEmail(t *testing.T) {
	f := newFakeProvider(t)
	provider := oauth.GitHub(credentials())
	provider.TokenURL, provider.UserURL, provider.EmailsURL = f.server.URL+"/token", f.server.URL+"/user", f.server.URL+"/user/emails"
	now := testkit.NewClock(time.Now())
	client, err := oauth.NewClient(provider, outbound(t, f.server), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, emails := range []int{200, 403} {
		f.emails = emails
		started, err := client.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(started.URL, "nonce=") {
			t.Fatal("OAuth-only provider received an OpenID nonce")
		}
		callback, _ := oauth.ParseCallback(f.authorize(t, started.URL, "good"))
		profile, err := client.Complete(t.Context(), started.Pending, callback)
		if err != nil {
			t.Fatal(err)
		}
		if profile.Provider != "github" || profile.Subject != "12345" || profile.Username != "octo" || profile.Name != "octo" || !profile.Token.HasScope("user:email") {
			t.Fatal("unexpected GitHub profile", profile.Subject)
		}
		if emails == 200 && (profile.Email != "octo@example.test" || !profile.EmailVerified) {
			t.Fatal("verified primary email was not used")
		}
		if emails == 403 && (profile.Email != "public@example.test" || profile.EmailVerified) {
			t.Fatal("public profile email was treated as verified")
		}
	}
}

// The encrypted pending cookie carries the request between redirect and
// callback; it is cleared on use, and a callback without it is rejected.
func TestFlowKeepsPendingRequestInEncryptedCookie(t *testing.T) {
	f := newFakeProvider(t)
	key := rsaSigner(t, "rsa1")
	f.keys = []signer{key}
	now := testkit.NewClock(time.Now())
	f.idToken = func(nonce string) string { return key.sign(t, nil, claimsAt(now.Now(), nonce)) }
	client, err := oauth.NewClient(googleLike(f), outbound(t, f.server), now)
	if err != nil {
		t.Fatal(err)
	}
	material, err := encryption.GenerateKey("oauth_test")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring("oauth_test", material)
	if err != nil {
		t.Fatal(err)
	}
	encrypter, err := foundryhttp.NewCookieEncrypter(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := oauth.NewFlow(client, encrypter, "__Host-foundry_oauth_google")
	if err != nil {
		t.Fatal(err)
	}
	start := httptest.NewRecorder()
	location, err := flow.Start(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}
	cookies := start.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != int(oauth.DefaultPendingLifetime/time.Second) || strings.Contains(cookies[0].Value, "state") {
		t.Fatal("pending cookie policy", cookies)
	}
	query := f.authorize(t, location, "good")
	finish := func(path string, withCookie bool) (oauth.Profile, *httptest.ResponseRecorder, error) {
		request := httptest.NewRequest("GET", "https://app.test"+path+"?"+query.Encode(), nil)
		if withCookie {
			request.AddCookie(cookies[0])
		}
		recorder := httptest.NewRecorder()
		profile, err := flow.Finish(recorder, request)
		return profile, recorder, err
	}
	if _, _, err := finish("/other/callback", true); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("callback on another path accepted", err)
	}
	profile, recorder, err := finish("/auth/callback", true)
	if err != nil || profile.Subject != "user-42" {
		t.Fatal(err)
	}
	if cleared := recorder.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatal("pending cookie was not cleared")
	}
	if _, _, err := finish("/auth/callback", false); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("callback without the pending cookie accepted", err)
	}
}

// Providers and outbound clients are validated before use.
func TestProviderAndClientValidation(t *testing.T) {
	f := newFakeProvider(t)
	unrestricted, err := httpclient.New(httpclient.DefaultConfig("oauth.open"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unrestricted.Close(t.Context()) })
	if _, err := oauth.NewClient(googleLike(f), unrestricted, testkit.NewClock(time.Now())); !errors.Is(err, fault.Invalid) {
		t.Fatal("unrestricted outbound client accepted", err)
	}
	for name, change := range map[string]func(*oauth.Provider){
		"http":     func(p *oauth.Provider) { p.TokenURL = "http://provider.example/token" },
		"redirect": func(p *oauth.Provider) { p.Credentials.RedirectURL = "https://app.test/cb?state=x" },
		"relative": func(p *oauth.Provider) { p.Credentials.RedirectURL = "/auth/callback" },
		"openid":   func(p *oauth.Provider) { p.Scopes = []string{"email"} },
		"issuers":  func(p *oauth.Provider) { p.Issuers = nil },
		"client":   func(p *oauth.Provider) { p.Credentials.ClientID = "" },
		"source":   func(p *oauth.Provider) { p.Source = 0 },
	} {
		provider := googleLike(f)
		change(&provider)
		if provider.Validate() == nil {
			t.Fatal("invalid provider accepted:", name)
		}
	}
	for _, provider := range []oauth.Provider{oauth.Google(credentials()), oauth.GitHub(credentials())} {
		policy, err := provider.Destinations()
		if err != nil || policy.Mode != httpclient.RestrictedDestinations || len(policy.Hosts) == 0 || policy.Ports[0] != 443 {
			t.Fatal("preset destinations are not restricted", provider.Name, err)
		}
	}
}
