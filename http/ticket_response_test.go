package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ticketWireTicket = token.Ticket[authAccount, int64]

// ticketWireBackend issues and looks up one token so a guarded scope can mint a
// ticket. Redemption and family re-checks are covered against PostgreSQL.
type ticketWireBackend struct {
	tokenWireBackend
	record *token.Record
}

func (b ticketWireBackend) Create(_ context.Context, address token.Address, creation token.Creation) (token.Record, error) {
	record, err := creation.At(address, b.now.Now())
	*b.record = record
	return record, err
}
func (b ticketWireBackend) Lookup(_ context.Context, _ token.Address, hash token.Digest, _ bool) (value.Optional[token.Record], error) {
	if !b.record.AccessHash.Equal(hash) {
		return value.Optional[token.Record]{}, nil
	}
	return value.Set(*b.record), nil
}
func (b ticketWireBackend) IssueTicket(_ context.Context, _ token.Address, _ model.Identity, _ model.ID[token.Record], _ token.Digest, lifetime time.Duration, _ int) (value.Optional[temporal.DateTime], error) {
	at, err := temporal.NewDateTime(b.now.Now().Add(lifetime))
	return value.Set(at), err
}
func (ticketWireBackend) RedeemTicket(context.Context, token.Address, token.Digest) (value.Optional[token.TicketRecord], error) {
	return value.Optional[token.TicketRecord]{}, nil
}
func (ticketWireBackend) LookupFamily(context.Context, token.Address, model.Identity, model.ID[token.Record]) (value.Optional[token.Record], error) {
	return value.Optional[token.Record]{}, nil
}
func (ticketWireBackend) PruneTickets(context.Context, token.Address, int) (uint64, error) {
	return 0, nil
}

func ticketWireIssue(t *testing.T) (ticketWireTicket, *testkit.Clock) {
	t.Helper()
	now := testkit.NewClock(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	store, err := token.NewStore(ticketWireBackend{tokenWireBackend{now: now}, new(token.Record)}, token.DefaultConfig(keyspace.Namespace{Application: "ticket-http", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("accounts", (authAccount{}).reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(_ context.Context, a authAccount) (bool, error) { return a.Enabled, nil })
	tokens, err := token.New(store, "api", provider, "bearer", auth.AccessScopes[authAccount]{})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(authAccount{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := tokens.Issue(t.Context(), proof, token.IssueOptions[authAccount]{Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := auth.NewRegistry(auth.DefaultConfig(), tokens.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "bearer", Secret: issued.AccessSecret()})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	ticket, err := tokens.IssueTicket(scope.Context())
	if err != nil {
		t.Fatal(err)
	}
	return ticket, now
}

func TestTicketDeliveryUsesTheCredentialBoundary(t *testing.T) {
	ticket, now := ticketWireIssue(t)
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "realtime.ticket", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/token")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.TicketResponse[authAccount, int64](201, now))
	router := newAuthRouter(t, endpoint.Handle(func(context.Context, authInput) (ticketWireTicket, error) { return ticket, nil }))
	now.Advance(1500 * time.Millisecond)
	w := tokenWireServe(router, "https")
	if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" {
		t.Fatal("ticket response headers", w.Code)
	}
	var wire struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil || wire.Ticket != ticket.Secret().Reveal() || wire.ExpiresIn != 28 {
		t.Fatal("ticket wire contract", w.Body.String(), err)
	}
	raw, err := json.Marshal(ticket)
	if err != nil || strings.Contains(string(raw), wire.Ticket) || strings.Contains(fmt.Sprintf("%#v", ticket), wire.Ticket) {
		t.Fatal("ordinary serialization disclosed the ticket")
	}
	if w := tokenWireServe(router, "http"); w.Code != 400 || strings.Contains(w.Body.String(), wire.Ticket) {
		t.Fatal("ticket delivered without TLS", w.Code)
	}
	now.Advance(token.DefaultConfig(keyspace.Namespace{}).TicketLifetime)
	if w := tokenWireServe(router, "https"); w.Code == 201 || strings.Contains(w.Body.String(), wire.Ticket) {
		t.Fatal("expired ticket delivered", w.Code)
	}
	get := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "unsafe", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.TicketResponse[authAccount, int64](200, now))
	if get.Validate() == nil {
		t.Fatal("ticket delivery accepted GET")
	}
}
