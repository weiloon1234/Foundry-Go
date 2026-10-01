package websocket_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/value"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

// fakeTickets is a single-use in-memory redeemer bound to one guard strategy.
type fakeTickets struct {
	source auth.CredentialName
	binder auth.Binder[int64]
	mu     sync.Mutex
	issued map[string]int64
	fail   error
}

func (f *fakeTickets) TicketSource() auth.CredentialName { return f.source }
func (f *fakeTickets) RedeemTicket(_ context.Context, raw secret.String) (value.Optional[auth.BoundCredential], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return value.Optional[auth.BoundCredential]{}, f.fail
	}
	id, ok := f.issued[raw.Reveal()]
	if !ok {
		return value.Optional[auth.BoundCredential]{}, nil
	}
	delete(f.issued, raw.Reveal())
	bound, err := f.binder.Bind(id)
	return value.Set(bound), err
}
func (f *fakeTickets) issue(ticket string, id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issued[ticket] = id
}

type ticketFixture struct {
	transport     *foundryhttp.Authentication
	users, admins auth.Guard[Account]
	userTickets   *fakeTickets
	adminTickets  *fakeTickets
	revoked       *atomic.Bool
}

func ticketAuthentication(t *testing.T) ticketFixture {
	t.Helper()
	revoked := new(atomic.Bool)
	provider := auth.DefineProvider("accounts", (Account{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[Account], error) {
		return value.Set(Account{ID: id, Display: "safe-name"}), nil
	}, func(context.Context, Account) (bool, error) { return true, nil })
	guard := func(name auth.GuardName, source auth.CredentialName, bearer string) (auth.Guard[Account], *fakeTickets) {
		strategy, binder := auth.BindStrategy(auth.DefineStrategy(source, func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[Account, int64]], error) {
			if credential.Reveal() != bearer {
				return value.Optional[auth.Proof[Account, int64]]{}, nil
			}
			proof, err := auth.NewProof(Account{ID: 7}.FoundryReference(), auth.Authenticated)
			return value.Set(proof), err
		}), func(_ context.Context, id int64) (value.Optional[auth.Proof[Account, int64]], error) {
			// Each scope re-checks the bound family; revocation ends it.
			if revoked.Load() {
				return value.Optional[auth.Proof[Account, int64]]{}, auth.Unauthenticated
			}
			proof, err := auth.NewProof(Account{ID: id}.FoundryReference(), auth.Authenticated)
			return value.Set(proof), err
		})
		return auth.DefineGuard(name, provider, strategy), &fakeTickets{source: source, binder: binder, issued: make(map[string]int64)}
	}
	users, userTickets := guard("users", "bearer", "valid-user")
	admins, adminTickets := guard("admins", "admin-bearer", "valid-admin")
	r, err := auth.NewRegistry(auth.DefaultConfig(), users.Registration(), admins.Registration())
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := foundryhttp.NewAuthentication(r, foundryhttp.BearerCredential("bearer"), foundryhttp.BearerCredential("admin-bearer"))
	if err != nil {
		t.Fatal(err)
	}
	return ticketFixture{adapter, users, admins, userTickets, adminTickets, revoked}
}

func (f serverFixture) dialTicket(t *testing.T, ticket string) (*client.Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := client.DialTicket(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", http.Header{"Origin": []string{f.server.URL}}, 64<<10, secret.New(ticket))
	if err == nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

// handshakeStatus offers raw subprotocol entries and reports the rejection status.
func (f serverFixture) handshakeStatus(t *testing.T, suffix string, headers http.Header, protocols ...string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Origin", f.server.URL)
	socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws"+suffix, &transport.DialOptions{HTTPHeader: headers, Subprotocols: protocols})
	if socket != nil {
		socket.CloseNow()
	}
	if response == nil {
		t.Fatal("handshake returned no response", err)
	}
	if response.Body != nil {
		response.Body.Close()
	}
	return response.StatusCode
}

func TestTicketHandshakeAuthenticatesOnlyItsGuardAndRefreshRevokes(t *testing.T) {
	a := ticketAuthentication(t)
	config := ws.DefaultConfig()
	config.AuthRefreshInterval = 100 * time.Millisecond
	config.OperationTimeout = time.Second
	users := ws.OwnedRooms[AccountChannel]("accounts", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.users, (Account{}).FoundryReference())
	admins := ws.OwnedRooms[AdminChannel]("admins", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.admins, (Account{}).FoundryReference())
	f := serveWith(t, registry(t, ws.Register(users), ws.Register(admins)), a.transport, config, nil, ws.WithTickets(a.userTickets, a.adminTickets))

	a.userTickets.issue("user-ticket", 7)
	peer, err := f.dialTicket(t, "user-ticket")
	if err != nil {
		t.Fatal(err)
	}
	subscribe(t, peer, "own", users.ID(), room("7"))
	// The user ticket authenticates only the users guard.
	send(t, peer, ws.Request{Action: ws.Subscribe, ID: "other-guard", Channel: admins.ID(), Room: room("7")})
	if r := receive(t, peer, ws.ErrorResponse); r.Code != ws.Unauthenticated {
		t.Fatal("ticket crossed into another guard", r.Code)
	}
	if _, err := f.dialTicket(t, "user-ticket"); err == nil {
		t.Fatal("replayed ticket accepted")
	}
	if _, err := f.dialTicket(t, "unknown-ticket"); err == nil {
		t.Fatal("unknown ticket accepted")
	}
	a.adminTickets.issue("admin-ticket", 7)
	admin, err := f.dialTicket(t, "admin-ticket")
	if err != nil {
		t.Fatal(err)
	}
	send(t, admin, ws.Request{Action: ws.Subscribe, ID: "user-channel", Channel: users.ID(), Room: room("7")})
	if r := receive(t, admin, ws.ErrorResponse); r.Code != ws.Unauthenticated {
		t.Fatal("admin ticket authenticated the users guard", r.Code)
	}

	a.userTickets.issue("query-ticket", 7)
	a.userTickets.issue("ambiguous", 7)
	for name, test := range map[string]struct {
		suffix    string
		headers   http.Header
		protocols []string
		status    int
	}{
		"ticket in query":   {"?ticket=query-ticket", nil, []string{ws.Subprotocol}, 400},
		"ticket without v1": {"", nil, []string{ws.TicketSubprotocolPrefix + "query-ticket"}, 400},
		"two tickets":       {"", nil, []string{ws.Subprotocol, ws.TicketSubprotocolPrefix + "a", ws.TicketSubprotocolPrefix + "b"}, 400},
		"ticket and header": {"", http.Header{"Authorization": []string{"Bearer valid-user"}}, []string{ws.Subprotocol, ws.TicketSubprotocolPrefix + "ambiguous"}, 401},
		"empty ticket":      {"", nil, []string{ws.Subprotocol, ws.TicketSubprotocolPrefix}, 400},
		"unknown ticket":    {"", nil, []string{ws.Subprotocol, ws.TicketSubprotocolPrefix + "missing"}, 401},
	} {
		if status := f.handshakeStatus(t, test.suffix, test.headers, test.protocols...); status != test.status {
			t.Fatalf("%s: status %d, want %d", name, status, test.status)
		}
	}
	// A query-string ticket never reached redemption, so it still works once.
	if _, err := f.dialTicket(t, "query-ticket"); err != nil {
		t.Fatal("rejected query attempt consumed the ticket", err)
	}

	// A transient redeemer failure is retryable, not an authentication failure.
	a.userTickets.mu.Lock()
	a.userTickets.fail = fault.New(fault.Overloaded, "store unavailable")
	a.userTickets.mu.Unlock()
	if status := f.handshakeStatus(t, "", nil, ws.Subprotocol, ws.TicketSubprotocolPrefix+"any"); status != 503 {
		t.Fatal("transient ticket failure was not retryable", status)
	}
	// Another guard's failing store does not block this guard's ticket.
	a.adminTickets.issue("admin-during-outage", 7)
	if _, err := f.dialTicket(t, "admin-during-outage"); err != nil {
		t.Fatal("one redeemer's failure blocked another guard's ticket", err)
	}
	a.userTickets.mu.Lock()
	a.userTickets.fail = nil
	a.userTickets.mu.Unlock()

	// Native Authorization-header clients keep working unchanged.
	native := f.dial(t, userHeaders())
	subscribe(t, native, "native", users.ID(), room("7"))

	// Revoking the underlying credential disconnects the ticket socket at the
	// next authorization refresh.
	a.revoked.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		if _, err := peer.Receive(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("revoked ticket connection stayed open")
			}
			break
		}
	}
}

func TestTicketsRequireHubAuthenticationAndUniqueSources(t *testing.T) {
	a := ticketAuthentication(t)
	r := registry(t, ws.Register(publicChannel()))
	if _, err := ws.New(r, nil, ws.DefaultConfig(), ws.WithTickets(a.userTickets)); !errors.Is(err, fault.Invalid) {
		t.Fatal("tickets accepted without hub authentication", err)
	}
	if _, err := ws.New(r, a.transport, ws.DefaultConfig(), ws.WithTickets(a.userTickets, a.userTickets)); !errors.Is(err, fault.Duplicate) {
		t.Fatal("repeated ticket source accepted", err)
	}
	f := serve(t, r, a.transport, ws.DefaultConfig())
	a.userTickets.issue("unsupported", 7)
	if status := f.handshakeStatus(t, "", nil, ws.Subprotocol, ws.TicketSubprotocolPrefix+"unsupported"); status != 401 {
		t.Fatal("hub without tickets accepted one", status)
	}
	if description := ws.ProtocolDescription(); description.TicketPrefix != ws.TicketSubprotocolPrefix {
		t.Fatal("ticket prefix is not exported to clients")
	}
}
