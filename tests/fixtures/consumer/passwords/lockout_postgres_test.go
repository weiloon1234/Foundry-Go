package passwords_test

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	lockmemory "github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	ratememory "github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestConsumerPasswordLockoutAndRequestRateComposition(t *testing.T) {
	passwordModels(t, func(tx *database.Tx) error {
		config := password.DefaultConfig()
		config.Parameters = password.Parameters{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
		hasher, err := password.New(config)
		if err != nil {
			return err
		}
		plain, err := password.NewPlaintext(secret.New("correct input"))
		if err != nil {
			return err
		}
		_, err = passwords.CreateAccount(t.Context(), tx, hasher, passwords.LoginRequest{Email: "member@example.test", Password: plain})
		if err != nil {
			return err
		}
		login, err := passwords.NewLogin(tx, passwords.Accounts(tx), hasher, func(context.Context, passwords.Account) (bool, error) { return false, nil })
		if err != nil {
			return err
		}
		clock := testkit.NewClock(time.Unix(1000, 0))
		backend, err := lockmemory.New(100, clock)
		if err != nil {
			return err
		}
		defer backend.Close()
		namespace := keyspace.Namespace{Application: "consumer", Environment: "password"}
		store, err := lockout.NewStore(backend, lockout.DefaultConfig(namespace))
		if err != nil {
			return err
		}
		login, err = passwords.ProtectLogin(login, store)
		if err != nil {
			return err
		}
		rateBackend, err := ratememory.New(100, clock)
		if err != nil {
			return err
		}
		defer rateBackend.Close()
		rates, err := ratelimit.NewStore(rateBackend, ratelimit.DefaultConfig(namespace))
		if err != nil {
			return err
		}
		handler, err := passwords.VerifyRoute(login, rates)
		if err != nil {
			return err
		}
		call := func(email, input, ip string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("POST", "https://consumer.test/verify", strings.NewReader(`{"email":"`+email+`","password":"`+input+`"}`))
			r.RemoteAddr = ip
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		for i := 0; i < 5; i++ {
			w := call("member@example.test", "wrong input", "192.0.2.1:9000")
			want := 401
			if i == 4 {
				want = 429
			}
			if w.Code != want {
				t.Fatalf("password failure %d: status %d", i, w.Code)
			}
		}
		if w := call("member@example.test", "correct input", "192.0.2.1:9000"); w.Code != 429 || w.Header().Get("Retry-After") != "900" {
			t.Fatal("locked client reached password verification", w.Code, w.Header())
		}
		// Client-aware lockout: failures from one source cannot lock the account
		// for everyone. The account-wide and per-IP ceilings still apply.
		if w := call("member@example.test", "correct input", "192.0.2.2:9000"); w.Code != 204 {
			t.Fatal("one client locked the account for another", w.Code)
		}
		throttle, err := passwords.PasswordAttempts.Bind(store)
		if err != nil {
			return err
		}
		origin, err := (attribution.Origin{}).WithRequest(attribution.Request{IP: netip.MustParseAddr("192.0.2.1")})
		if err != nil {
			return err
		}
		locked, err := attribution.WithContext(t.Context(), origin)
		if err != nil {
			return err
		}
		if _, err := passwords.ClearPasswordFailures(locked, throttle, "member@example.test"); err != nil {
			return err
		}
		if w := call("member@example.test", "correct input", "192.0.2.1:9000"); w.Code != 204 {
			t.Fatal("reset did not restore admission", w.Code)
		}
		// Every attempt still consumes the IP quota, including successful passwords.
		for range 9 {
			if w := call("member@example.test", "correct input", "192.0.2.2:9000"); w.Code != 204 {
				t.Fatal("unexpected quota exhaustion", w.Code)
			}
		}
		if w := call("member@example.test", "correct input", "192.0.2.2:9000"); w.Code != 429 || w.Header().Get("X-RateLimit-Remaining") != "0" {
			t.Fatal("network quota was cleared by password success", w.Code, w.Header())
		}
		return nil
	})
}
