package recovering_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	limitmemory "github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"github.com/weiloon1234/Foundry-Go/value"
)

func requestLimitStore(t *testing.T, s *fixture) *ratelimit.Store {
	t.Helper()
	memory, err := limitmemory.New(64, s.clock)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ratelimit.NewStore(memory, ratelimit.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func requestRecipientLimit(t *testing.T, store *ratelimit.Store, name ratelimit.Name, count uint32) ratelimit.Limiter[string] {
	t.Helper()
	limiter, err := ratelimit.Define(name, keyspace.StringKeys[string](), ratelimit.PerHour(count)).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}
func requestLookup(s *fixture) recovering.RecipientLookup {
	return func(ctx context.Context, email string) (value.Optional[model.Reference[recovering.Member, model.ID[recovering.Member]]], error) {
		var result value.Optional[model.Reference[recovering.Member, model.ID[recovering.Member]]]
		err := s.within(ctx, func(tx *database.Tx) error {
			found, err := recovering.QueryRecoveryMembers().Where(recovering.MemberFields().Email.Eq(email)).First(ctx, tx)
			if err != nil {
				return err
			}
			if member, ok := found.Get(); ok {
				result = value.Set(member.FoundryReference())
			}
			return nil
		})
		return result, err
	}
}
func TestPostgresRecoveryLinkRequestsUseCommittedAddressAndQuota(t *testing.T) {
	s := prepare(t)
	store := requestLimitStore(t, s)
	var logs bytes.Buffer
	var delivery passwordreset.Issued[recovering.Member]
	sent := 0
	lookup := requestLookup(s)
	requests, err := recovering.NewResetRequests(s.reset, requestRecipientLimit(t, store, "reset", 1), func(ctx context.Context, input string) (value.Optional[model.Reference[recovering.Member, model.ID[recovering.Member]]], error) {
		found, err := lookup(ctx, input)
		if err != nil {
			return found, err
		}
		// The account changes after the email locator. Issuance must lock/read
		// current stored email, and delivery must use that committed snapshot.
		if found.IsSet() {
			err = s.within(ctx, func(tx *database.Tx) error {
				_, err := recovering.QueryRecoveryMembers().Update(ctx, tx, s.member.ID, recovering.MemberDraft{}.SetEmail("current@example.test"))
				return err
			})
		}
		return found, err
	}, func(ctx context.Context, issued passwordreset.Issued[recovering.Member]) error {
		sent++
		delivery = issued
		if issued.Subject().Email != "current@example.test" {
			return errors.New("delivery used submitted address")
		}
		// A separate transaction can consume here: Issue has released its locks
		// and committed. This is a test transport, not an external message send.
		_, err := s.reset.Complete(ctx, issued.Token(), plain(t, "delivered long password"))
		return err
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = requests.Close(context.Background()) })
	// Delivery is owned background work, so response latency does not reveal
	// whether the account exists; Wait observes the completed dispatch.
	if err := requests.Request(t.Context(), "member@example.test"); err != nil || requests.Wait(t.Context()) != nil || sent != 1 {
		t.Fatal("request did not deliver", err)
	}
	if delivery.Subject().Email != "current@example.test" || current(t, s).Password == s.member.Password || logs.Len() != 0 {
		t.Fatal("delivery was not after commit or used wrong address")
	}
	if err := requests.Request(t.Context(), "member@example.test"); err != nil || requests.Wait(t.Context()) != nil || sent != 1 {
		t.Fatal("quota repeated lookup/delivery", err)
	}
	if _, err := s.reset.Complete(t.Context(), delivery.Token(), plain(t, "another long password")); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("test delivery did not consume committed token", err)
	}
}
func TestPostgresRecoveryRequestHTTPHasUniformAcknowledgements(t *testing.T) {
	s := prepare(t)
	store := requestLimitStore(t, s)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	sent := 0
	fail := false
	reset, err := recovering.NewResetRequests(s.reset, requestRecipientLimit(t, store, "reset", 2), requestLookup(s), func(context.Context, passwordreset.Issued[recovering.Member]) error {
		sent++
		if fail {
			return errors.New("private delivery failure")
		}
		return nil
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	verify, err := recovering.NewVerificationRequests(s.verification, requestRecipientLimit(t, store, "verify", 2), requestLookup(s), func(context.Context, emailverification.Issued[recovering.Member]) error { sent++; return nil }, logger)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := ratelimit.Define("ingress", keyspace.TextKeys[netip.Addr](), ratelimit.PerMinute(20)).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reset.Close(context.Background()); _ = verify.Close(context.Background()) })
	router, err := recovering.LinkRoutes(reset, verify, ingress)
	if err != nil {
		t.Fatal(err)
	}
	settle := func() {
		if err := reset.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := verify.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"/recovery/request-reset", "/recovery/request-verification"}
	for _, path := range paths {
		for _, email := range []string{"member@example.test", "absent@example.test"} {
			response := recoveryHTTP(router, "POST", "https", path, `{"email":"`+email+`"}`, "https://app.test")
			settle()
			if response.Code != 204 || response.Body.Len() != 0 || len(response.Result().Cookies()) != 0 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("request disclosed outcome", response.Code)
			}
		}
	}
	if sent != 2 {
		t.Fatal("missing account reached delivery", sent)
	}
	fail = true
	response := recoveryHTTP(router, "POST", "https", paths[0], `{"email":"member@example.test"}`, "https://app.test")
	settle()
	if response.Code != 204 || sent != 3 || logs.Len() == 0 {
		t.Fatal("delivery failure changed public acknowledgement", response.Code)
	}
	quota := recoveryHTTP(router, "POST", "https", paths[0], `{"email":"member@example.test"}`, "https://app.test")
	settle()
	if quota.Code != 204 || sent != 3 {
		t.Fatal("recipient quota leaked or retried")
	}
	invalid := recoveryHTTP(router, "POST", "https", paths[0], `{"email":"not an email"}`, "https://app.test")
	if invalid.Code != 422 || sent != 3 {
		t.Fatal("invalid email reached lookup", invalid.Code)
	}
	update(t, s, recovering.MemberDraft{}.SetEnabled(false))
	disabled := recoveryHTTP(router, "POST", "https", paths[1], `{"email":"member@example.test"}`, "https://app.test")
	settle()
	if disabled.Code != 204 || sent != 3 {
		t.Fatal("disabled account disclosed or delivered", disabled.Code)
	}
	if strings.Contains(logs.String(), "member@example.test") || strings.Contains(logs.String(), "private delivery failure") {
		t.Fatal("diagnostics exposed recovery details")
	}
}
