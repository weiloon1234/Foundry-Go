package lockout_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

func from(t *testing.T, ip string) context.Context {
	t.Helper()
	request := attribution.Request{}
	if ip != "" {
		request.IP = netip.MustParseAddr(ip)
	}
	origin, err := (attribution.Origin{}).WithRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func login(t *testing.T, s *lockout.Store, limits lockout.Limits) lockout.Throttle[email] {
	t.Helper()
	th, err := lockout.DefineLogin("password.accounts", keyspace.StringKeys[email](), limits).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func wrong(context.Context) (bool, error) { return false, nil }
func right(context.Context) (bool, error) { return true, nil }

func TestClientAwareLockoutCannotLockVictimOutFromOneSource(t *testing.T) {
	b, _ := local(t)
	limits := lockout.DefaultLimits()
	th := login(t, store(t, b), limits)
	attacker := from(t, "203.0.113.7")
	for range limits.PerClient.MaxFailures - 1 {
		if ok, err := th.Run(attacker, "victim@example.test", wrong); ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	var rejection *lockout.Rejection
	if ok, err := th.Run(attacker, "victim@example.test", wrong); ok || !errors.As(err, &rejection) || !rejection.Triggered() {
		t.Fatal("pair threshold did not lock the attacking client", ok, err)
	}
	if ok, err := th.Run(attacker, "victim@example.test", func(context.Context) (bool, error) { t.Error("locked client reached verifier"); return true, nil }); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal(ok, err)
	}
	if ok, err := th.Run(from(t, "198.51.100.20"), "victim@example.test", right); !ok || err != nil {
		t.Fatal("one client locked the account for everyone", ok, err)
	}
	// The attacker's other targets are still limited only by their own windows.
	if ok, err := th.Run(attacker, "other@example.test", right); !ok || err != nil {
		t.Fatal(ok, err)
	}
}

// The per-address ceiling is off by default: many users behind one shared
// address (carrier-grade NAT, a corporate proxy) keep logging in even after
// that address produced many failures across accounts.
func TestDefaultLimitsDoNotLockSharedAddresses(t *testing.T) {
	b, _ := local(t)
	limits := lockout.DefaultLimits()
	if limits.Address.IsSet() {
		t.Fatal("per-address ceiling is enabled by default")
	}
	th := login(t, store(t, b), limits)
	shared := from(t, "198.51.100.200")
	// More failures from one address than the opt-in ceiling would allow.
	perAccount := limits.PerClient.MaxFailures - 1
	for i := range lockout.DefaultAddressPolicy().MaxFailures/perAccount + 1 {
		account := email(fmt.Sprintf("user%d@example.test", i))
		for range perAccount {
			if ok, err := th.Run(shared, account, wrong); ok || err != nil {
				t.Fatal(ok, err)
			}
		}
	}
	if ok, err := th.Run(shared, "colleague@example.test", right); !ok || err != nil {
		t.Fatal("a shared address locked out a correct password", ok, err)
	}
	enabled := limits
	enabled.Address = value.Set(lockout.Policy{MaxFailures: 1, Window: time.Minute, LockFor: time.Minute})
	if enabled.Validate() == nil {
		t.Fatal("address ceiling below the per-client threshold accepted")
	}
}

func TestClientAwareCeilingsBoundDistributedAndSprayingAttempts(t *testing.T) {
	b, _ := local(t)
	limits := lockout.Limits{
		PerClient: lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: time.Minute},
		Account:   lockout.Policy{MaxFailures: 3, Window: time.Minute, LockFor: time.Minute},
		Address:   value.Set(lockout.Policy{MaxFailures: 4, Window: time.Minute, LockFor: time.Minute}),
	}
	th := login(t, store(t, b), limits)
	for _, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} {
		_, _ = th.Run(from(t, ip), "target@example.test", wrong)
	}
	if ok, err := th.Run(from(t, "192.0.2.4"), "target@example.test", right); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal("account ceiling did not stop distributed guessing", ok, err)
	}
	sprayer := from(t, "192.0.2.50")
	for i, account := range []email{"a@example.test", "b@example.test", "c@example.test"} {
		if ok, err := th.Run(sprayer, account, wrong); ok || err != nil {
			t.Fatal(i, ok, err)
		}
	}
	if ok, err := th.Run(sprayer, "d@example.test", wrong); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal("address ceiling did not trigger", ok, err)
	}
	if ok, err := th.Run(sprayer, "own@example.test", right); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal("address ceiling allowed a new account from the spraying source", ok, err)
	}
	if ok, err := th.Run(from(t, "192.0.2.51"), "own@example.test", right); !ok || err != nil {
		t.Fatal("address ceiling leaked to another source", ok, err)
	}
}

func TestClientAwareSuccessResetAndUnknownClients(t *testing.T) {
	b, _ := local(t)
	limits := lockout.Limits{
		PerClient: lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: time.Minute},
		Account:   lockout.Policy{MaxFailures: 3, Window: time.Minute, LockFor: time.Minute},
		Address:   value.Set(lockout.Policy{MaxFailures: 3, Window: time.Minute, LockFor: time.Minute}),
	}
	th := login(t, store(t, b), limits)
	client := from(t, "2001:db8::1")
	// Success clears the pair and account windows but not the address ceiling.
	_, _ = th.Run(client, "member@example.test", wrong)
	_, _ = th.Run(client, "member@example.test", wrong)
	if ok, err := th.Run(client, "member@example.test", right); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal(ok, err)
	}
	if changed, err := th.Reset(client, "member@example.test"); err != nil || !changed {
		t.Fatal("reset did not clear the account/pair", changed, err)
	}
	if ok, err := th.Run(client, "member@example.test", right); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := th.Run(client, "member@example.test", wrong); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal("success cleared the address ceiling", ok, err)
	}
	// No trusted client IP: one shared pair window per account and no address ceiling.
	anonymous := from(t, "")
	_, _ = th.Run(anonymous, "cli@example.test", wrong)
	if ok, err := th.Run(anonymous, "cli@example.test", wrong); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal(ok, err)
	}
	if ok, err := th.Run(from(t, "192.0.2.9"), "cli@example.test", right); !ok || err != nil {
		t.Fatal("unknown-client attempts locked identified clients", ok, err)
	}
}

func TestClientAwareObserverAndValidation(t *testing.T) {
	b, _ := local(t)
	limits := lockout.DefaultLimits()
	limits.PerClient.MaxFailures = 1
	th := login(t, store(t, b), limits)
	notices := 0
	th, err := th.WithLockedObserver(func(context.Context, lockout.Notice[email]) error { notices++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := th.Run(from(t, "192.0.2.10"), "member@example.test", wrong); ok || !errors.Is(err, lockout.Locked) || notices != 1 {
		t.Fatal(ok, err, notices)
	}
	if got, ok := th.Limits(); !ok || got != limits {
		t.Fatal("limits not retained")
	}
	invalid := lockout.DefaultLimits()
	invalid.Account.MaxFailures = invalid.PerClient.MaxFailures - 1
	if _, err := lockout.DefineLogin("password.invalid", keyspace.StringKeys[email](), invalid).Bind(store(t, b)); !errors.Is(err, fault.Invalid) {
		t.Fatal("ceiling below the per-client threshold accepted", err)
	}
}
