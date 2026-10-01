package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// issueTicket mints a ticket inside a scope authenticated by an access token,
// as a guarded issuance endpoint does.
func issueTicket(t *testing.T, s *setup, access secret.String) (token.Ticket[member, int64], error) {
	t.Helper()
	credentials, err := auth.NewCredentials(auth.Credential{Name: "api.bearer", Secret: access})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := s.registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	return s.tokens.IssueTicket(scope.Context())
}

// redeem exchanges a ticket for handshake credentials, as the hub does.
func redeem(t *testing.T, s *setup, tokens *token.Tokens[member, int64], raw secret.String) (auth.Credentials, bool) {
	t.Helper()
	found, err := tokens.RedeemTicket(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	bound, present := found.Get()
	if !present {
		return auth.Credentials{}, false
	}
	credentials, err := auth.Credentials{}.WithBound(tokens.TicketSource(), bound)
	if err != nil {
		t.Fatal(err)
	}
	return credentials, true
}

// boundScope re-verifies redeemed credentials in a fresh scope, as each
// subscribe and authorization refresh does.
func boundScope(t *testing.T, s *setup, credentials auth.Credentials) (token.Info[member, int64], error) {
	t.Helper()
	scope, err := s.registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	if _, err := s.tokens.Guard().RequireScopes(scope.Context(), s.grants); err != nil {
		return token.Info[member, int64]{}, err
	}
	return s.tokens.Current(scope.Context())
}

func ticketRows(t *testing.T, s *setup) int64 {
	t.Helper()
	var count int64
	if err := database.ScanOne(t.Context(), s.db, `SELECT count(*) FROM "`+s.schema+`".foundry_token_tickets`, nil, &count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPostgresTicketsAreSingleUseAndFollowTheirFamily(t *testing.T) {
	s := prepare(t)
	issued := issue(t, s, 7, true)
	ticket, err := issueTicket(t, s, issued.AccessSecret())
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Family() != issued.Info().ID() || !ticket.ExpiresAt().UTC().Equal(s.clock.Now().Add(s.config.TicketLifetime)) {
		t.Fatal("ticket is not bound to the issuing family and lifetime", ticket.ExpiresAt())
	}
	credentials, ok := redeem(t, s, s.tokens, ticket.Secret())
	if !ok {
		t.Fatal("issued ticket did not redeem")
	}
	if _, again := redeem(t, s, s.tokens, ticket.Secret()); again {
		t.Fatal("ticket redeemed twice")
	}
	info, err := boundScope(t, s, credentials)
	if err != nil || info.ID() != issued.Info().ID() || !info.Scopes().ContainsAll(s.grants) {
		t.Fatal("redeemed ticket did not authenticate its family", err)
	}
	// Access-token rotation keeps the family, so the bound connection stays.
	rotated, err := s.tokens.Refresh(t.Context(), refreshSecret(t, issued))
	if err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(s.config.AccessGrace + time.Second)
	if info, err := boundScope(t, s, credentials); err != nil || info.Generation() != rotated.Info().Generation() {
		t.Fatal("family re-check failed after an ordinary refresh", err)
	}
	// Current eligibility applies at every re-check.
	s.enabled.Store(false)
	if _, err := boundScope(t, s, credentials); err == nil {
		t.Fatal("disabled subject stayed authenticated")
	}
	s.enabled.Store(true)
	// A ticket of a revoked family no longer redeems, and revocation ends the
	// bound credential at its next scope.
	pending, err := issueTicket(t, s, rotated.AccessSecret())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.tokens.RevokeID(t.Context(), member{ID: 7}.reference(), issued.Info().ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := boundScope(t, s, credentials); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked family stayed authenticated", err)
	}
	if _, ok := redeem(t, s, s.tokens, pending.Secret()); ok || ticketRows(t, s) != 0 {
		t.Fatal("revoking a family kept its tickets")
	}
}

func TestPostgresTicketsExpireAreCappedAndStayInTheirGuard(t *testing.T) {
	s := prepare(t)
	issued := issue(t, s, 7, true)
	expired, err := issueTicket(t, s, issued.AccessSecret())
	if err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(s.config.TicketLifetime)
	if _, ok := redeem(t, s, s.tokens, expired.Secret()); ok {
		t.Fatal("expired ticket redeemed")
	}
	// At the cap the oldest unexpired ticket is dropped; expired ones go first.
	var tickets []token.Ticket[member, int64]
	for range s.config.MaxTicketsPerFamily + 1 {
		ticket, err := issueTicket(t, s, issued.AccessSecret())
		if err != nil {
			t.Fatal(err)
		}
		tickets = append(tickets, ticket)
		s.clock.Advance(time.Millisecond)
	}
	if rows := ticketRows(t, s); rows != int64(s.config.MaxTicketsPerFamily) {
		t.Fatal("ticket cap was not enforced", rows)
	}
	if _, ok := redeem(t, s, s.tokens, tickets[0].Secret()); ok {
		t.Fatal("oldest ticket survived the cap")
	}
	// Another guard's binding never redeems this guard's ticket.
	admins, err := token.New(s.store, "members.admin", s.provider, "admin.bearer", s.grants)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := redeem(t, s, admins, tickets[1].Secret()); ok {
		t.Fatal("ticket crossed into another guard")
	}
	if _, ok := redeem(t, s, s.tokens, tickets[1].Secret()); !ok {
		t.Fatal("cross-guard attempt consumed the ticket")
	}
	// Pruning removes expired tickets within its bound.
	s.clock.Advance(s.config.TicketLifetime)
	if _, err := s.tokens.Prune(t.Context(), token.MaxPruneFamilies); err != nil {
		t.Fatal(err)
	}
	if rows := ticketRows(t, s); rows != 0 {
		t.Fatal("expired tickets were not pruned", rows)
	}
	// A personal token's family ends at its expiry, and so does its ticket.
	personal := issue(t, s, 8, false)
	ticket, err := issueTicket(t, s, personal.AccessSecret())
	if err != nil {
		t.Fatal(err)
	}
	credentials, ok := redeem(t, s, s.tokens, ticket.Secret())
	if !ok {
		t.Fatal("personal token ticket did not redeem")
	}
	s.clock.Advance(s.config.Personal.Absolute)
	if _, err := boundScope(t, s, credentials); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("expired family stayed authenticated", err)
	}
}

func TestTicketIssuanceNeedsAnAuthenticatedTokenAndEnabledTickets(t *testing.T) {
	s := prepare(t)
	if _, err := s.tokens.IssueTicket(t.Context()); err == nil {
		t.Fatal("ticket issued without an authenticated token")
	}
	disabled := s.config
	disabled.TicketLifetime, disabled.MaxTicketsPerFamily = 0, 0
	store, err := token.NewStore(s.backend, disabled)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.New(store, "members.api", s.provider, "api.bearer", s.grants)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.RedeemTicket(t.Context(), secret.New("ticket")); !errors.Is(err, fault.Invalid) {
		t.Fatal("disabled tickets redeemed", err)
	}
	invalid := s.config
	invalid.TicketLifetime = token.MaxTicketLifetime + time.Second
	if err := invalid.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded ticket lifetime accepted")
	}
}
