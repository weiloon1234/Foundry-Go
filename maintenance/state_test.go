package maintenance_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

func TestRulesParseMatchAndRejectAmbiguousPaths(t *testing.T) {
	for _, test := range []struct {
		text, method, path string
		matches            bool
	}{
		{"/status", "GET", "/status", true},
		{"/status", "POST", "/status/db", false},
		{"/status/*", "POST", "/status/db", true},
		{"/status/*", "GET", "/statuses", false},
		{"/status/*", "GET", "/status", true},
		{"GET /health", "GET", "/health", true},
		{"GET /health", "HEAD", "/health", false},
		{"/*", "DELETE", "/any/thing", true},
	} {
		rule, err := maintenance.ParseRule(test.text)
		if err != nil {
			t.Fatal(test.text, err)
		}
		if rule.Matches(test.method, test.path) != test.matches || rule.String() != test.text {
			t.Fatal("rule matching or text form changed", test, rule)
		}
	}
	for _, text := range []string{"", "status", "/a//b", "/a/../b", "/a/", "get /a", "/a?x=1", "/%2e", "GET  /a"} {
		if _, err := maintenance.ParseRule(text); !errors.Is(err, fault.Invalid) {
			t.Fatal("ambiguous rule accepted", text, err)
		}
	}
}

func TestStateValidationAndVersionedRecordOmitSecret(t *testing.T) {
	secret, err := maintenance.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := maintenance.DigestSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	rule, _ := maintenance.ParseRule("POST /hooks/*")
	state := maintenance.State{Down: true, RetryAfter: time.Minute, Message: "Back soon", Secret: digest, Allow: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Exempt: []maintenance.Rule{rule}, Since: time.Unix(100, 0).UTC()}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), `"v":1`) {
		t.Fatal("record leaked the secret or lost its version", string(data))
	}
	var decoded maintenance.State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Secret != digest || decoded.RetryAfter != time.Minute || decoded.Message != "Back soon" || len(decoded.Allow) != 1 || decoded.Exempt[0] != rule || !decoded.Since.Equal(state.Since) {
		t.Fatal("record did not round trip", decoded)
	}
	for _, invalid := range []maintenance.State{
		{Down: true, RetryAfter: 1500 * time.Millisecond},
		{Down: true, RetryAfter: 25 * time.Hour},
		{Down: true, Message: "line\nbreak"},
		{Down: true, Message: strings.Repeat("x", maintenance.MaxMessageBytes+1)},
		{Down: false, Message: "stale"},
		{Down: true, Allow: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/8")}},
	} {
		if err := invalid.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid state accepted", invalid)
		}
	}
	for _, record := range []string{`{"v":2,"down":true}`, `{"v":1,"down":true,"extra":1}`, `{"v":1,"down":false,"message":"x"}`, `{"v":1,"down":true,"secret_sha256":"XYZ"}`} {
		if err := json.Unmarshal([]byte(record), &decoded); err == nil {
			t.Fatal("invalid record accepted", record)
		}
	}
	if _, err := maintenance.DigestSecret("short"); !errors.Is(err, fault.Invalid) {
		t.Fatal("weak secret accepted")
	}
}

func TestGateExemptionsBypassCookiesAndDrainPrecedence(t *testing.T) {
	gate, err := maintenance.New(maintenance.Policy{Exempt: []maintenance.Rule{{Method: "GET", Path: "/up"}}, Allow: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, BypassTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	peer, other := netip.MustParseAddr("192.0.2.7"), netip.MustParseAddr("198.51.100.1")
	if gate.Exempts("GET", "/up", other) {
		t.Fatal("serving gate reported maintenance exemptions")
	}
	digest, _ := maintenance.DigestSecret("operator-secret-value")
	rule, _ := maintenance.ParseRule("/api/status/*")
	if err := gate.Apply(maintenance.State{Down: true, Secret: digest, Exempt: []maintenance.Rule{rule}, Allow: []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")}}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(gate.Admit(), maintenance.ErrMaintenance) || !gate.State().Down {
		t.Fatal("applied state did not pause admission")
	}
	for _, test := range []struct {
		method, path string
		ip           netip.Addr
		want         bool
	}{
		{"GET", "/up", other, true}, {"POST", "/up", other, false}, {"GET", "/", peer, true},
		{"PUT", "/api/status/x", other, true}, {"GET", "/api", other, false},
		{"GET", "/", netip.MustParseAddr("2001:db8::1"), true}, {"GET", "/", netip.MustParseAddr("::ffff:192.0.2.9"), true},
	} {
		if gate.Exempts(test.method, test.path, test.ip) != test.want {
			t.Fatal("exemption decision changed", test)
		}
	}
	now := time.Unix(1_000_000, 0)
	ctx := t.Context()
	if _, _, ok := gate.IssueBypass(ctx, "wrong-secret-value-000", now); ok {
		t.Fatal("wrong secret issued a bypass")
	}
	cookie, expires, ok := gate.IssueBypass(ctx, "operator-secret-value", now)
	if !ok || !expires.Equal(now.Add(time.Hour)) || !gate.Bypass(ctx, cookie, now.Add(time.Minute)) || gate.Bypass(ctx, cookie, now.Add(2*time.Hour)) || gate.Bypass(ctx, tamper(cookie), now) {
		t.Fatal("bypass cookie lifetime or integrity failed")
	}
	rotated, _ := maintenance.DigestSecret("rotated-secret-value-1")
	if err := gate.Apply(maintenance.State{Down: true, Secret: rotated}); err != nil {
		t.Fatal(err)
	}
	if gate.Bypass(ctx, cookie, now) || gate.Exempts("PUT", "/api/status/x", other) {
		t.Fatal("rotated state retained the previous secret or rules")
	}
	gate.Drain()
	if gate.Exempts("GET", "/up", other) || gate.Bypass(ctx, cookie, now) || !errors.Is(gate.Apply(maintenance.State{}), maintenance.ErrDraining) {
		t.Fatal("drain was reopened by maintenance exemptions")
	}
	if _, err := maintenance.New(maintenance.Policy{Exempt: []maintenance.Rule{{Path: "/a"}, {Path: "/a"}}}); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate policy rule accepted")
	}
}

// A bypass cookie is sealed with the application keys, so the shared store's
// secret digest alone never mints one; instances without the keys (or with
// only a process-local key) reject it.
func TestBypassCookiesNeedTheApplicationKeys(t *testing.T) {
	key, err := encryption.GenerateKey("maintenance")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring(key.ID(), key)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := maintenance.DigestSecret("operator-secret-value")
	down := maintenance.State{Down: true, Secret: digest}
	gate := func(keys *encryption.Keyring) *maintenance.Gate {
		g, err := maintenance.New(maintenance.Policy{Keys: keys})
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Apply(down); err != nil {
			t.Fatal(err)
		}
		return g
	}
	ctx, now := t.Context(), time.Unix(1_000_000, 0)
	first, second, unkeyed := gate(keys), gate(keys), gate(nil)
	cookie, _, ok := first.IssueBypass(ctx, "operator-secret-value", now)
	if !ok || !second.Bypass(ctx, cookie, now) || unkeyed.Bypass(ctx, cookie, now) {
		t.Fatal("a sealed bypass must be valid on every instance sharing the keys only")
	}
	// The earlier scheme keyed the MAC with the stored digest itself.
	forged := hmac.New(sha256.New, digest[:])
	forged.Write([]byte("foundry.maintenance.bypass.v1\x00"))
	expiry := binary.BigEndian.AppendUint64(nil, uint64(now.Add(time.Hour).Unix()))
	forged.Write(expiry)
	value := base64.RawURLEncoding.EncodeToString(append(expiry, forged.Sum(nil)...))
	if first.Bypass(ctx, value, now) || unkeyed.Bypass(ctx, value, now) {
		t.Fatal("a cookie minted from the stored digest was accepted")
	}
	local, _, ok := unkeyed.IssueBypass(ctx, "operator-secret-value", now)
	if !ok || !unkeyed.Bypass(ctx, local, now) || gate(nil).Bypass(ctx, local, now) || first.Bypass(ctx, local, now) {
		t.Fatal("an unkeyed bypass must stay local to its instance")
	}
}

type memoryStore struct {
	mu     sync.Mutex
	state  maintenance.State
	found  bool
	fail   atomic.Bool
	loads  atomic.Int32
	saving error
}

func (s *memoryStore) Load(ctx context.Context) (maintenance.State, bool, error) {
	s.loads.Add(1)
	if s.fail.Load() {
		return maintenance.State{}, false, errors.New("private store failure")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.found, ctx.Err()
}
func (s *memoryStore) Save(_ context.Context, state maintenance.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saving != nil {
		return s.saving
	}
	s.state, s.found = state, true
	return nil
}

func TestSharedStoreRefreshPublishAndWatchRetainLastState(t *testing.T) {
	store := &memoryStore{}
	local, remote := &maintenance.Gate{}, &maintenance.Gate{}
	if err := local.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Refresh(t.Context(), local, store); err != nil || local.Mode() != maintenance.Paused {
		t.Fatal("absent shared state replaced configured maintenance", err)
	}
	if err := maintenance.Publish(t.Context(), remote, store, maintenance.State{Down: true, Message: "fleet"}); err != nil || remote.Mode() != maintenance.Paused {
		t.Fatal("publish did not apply locally", err)
	}
	store.saving = errors.New("private save failure")
	if err := maintenance.Publish(t.Context(), remote, store, maintenance.State{}); err == nil || remote.Mode() != maintenance.Paused {
		t.Fatal("failed publication changed local admission")
	}
	store.saving = nil
	observer := &maintenance.Gate{}
	ctx, cancel := context.WithCancel(t.Context())
	var reports atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- maintenance.Watch(ctx, observer, store, maintenance.MinPollInterval, func(error) { reports.Add(1) })
	}()
	eventually(t, func() bool { return observer.State().Message == "fleet" })
	store.fail.Store(true)
	loads := store.loads.Load()
	eventually(t, func() bool { return store.loads.Load() > loads+2 })
	if reports.Load() != 1 || observer.Mode() != maintenance.Paused {
		t.Fatal("store outage changed admission or repeated its report", reports.Load())
	}
	store.fail.Store(false)
	if err := maintenance.Publish(t.Context(), remote, store, maintenance.State{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return observer.Mode() == maintenance.Serving })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not stop")
	}
	if err := maintenance.Watch(t.Context(), observer, store, time.Millisecond, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded poll interval accepted")
	}
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func tamper(value string) string {
	replacement := byte('A')
	if value[10] == 'A' {
		replacement = 'B'
	}
	return value[:10] + string(replacement) + value[11:]
}
