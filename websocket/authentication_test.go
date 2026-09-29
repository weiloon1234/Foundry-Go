package websocket_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

type Account struct {
	ID      int64
	Display string
	Secret  string
}

func (a Account) FoundryReference() model.Reference[Account, int64] {
	return model.NewReference[Account]("ws_accounts", a.ID, codec.Signed[int64]())
}
func (a Account) FoundryIdentity() (model.Identity, error) { return a.FoundryReference().Identity() }
func (Account) AccessID() string                           { panic("presentation getters must not authorize rooms") }

type AccountChannel struct{}
type AdminChannel struct{}
type PresenceOwner struct{}

type authFixture struct {
	transport     *foundryhttp.Authentication
	users, admins auth.Guard[Account]
	disabled      *atomic.Bool
	loads         *atomic.Int32
}

func authentication(t *testing.T) authFixture {
	t.Helper()
	disabled := new(atomic.Bool)
	loads := new(atomic.Int32)
	provider := auth.DefineProvider("accounts", (Account{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[Account], error) {
		loads.Add(1)
		return value.Set(Account{ID: id, Display: "safe-name", Secret: "private-model-field"}), nil
	}, func(context.Context, Account) (bool, error) { return !disabled.Load(), nil })
	proof, err := auth.NewProof(Account{ID: 7}.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := func(name auth.CredentialName, expected string) auth.Strategy[Account, int64] {
		return auth.DefineStrategy(name, func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[Account, int64]], error) {
			if credential.Reveal() != expected {
				return value.Optional[auth.Proof[Account, int64]]{}, nil
			}
			return value.Set(proof), nil
		})
	}
	users := auth.DefineGuard("users", provider, strategy("bearer", "valid-user"))
	admins := auth.DefineGuard("admins", provider, strategy("admin-cookie", "valid-admin"))
	r, err := auth.NewRegistry(auth.DefaultConfig(), users.Registration(), admins.Registration())
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := foundryhttp.DefineCookie("admin_session", foundryhttp.SecretCookie(), foundryhttp.DefaultCookieOptions())
	adapter, err := foundryhttp.NewAuthentication(r, foundryhttp.BearerCredential("bearer"), foundryhttp.CookieCredential("admin-cookie", adminCookie).WithoutOriginProtection())
	if err != nil {
		t.Fatal(err)
	}
	return authFixture{adapter, users, admins, disabled, loads}
}
func userHeaders() http.Header { return http.Header{"Authorization": []string{"Bearer valid-user"}} }

func TestPrivateRoomsBindIdentityGuardAndFreshAuthorization(t *testing.T) {
	a := authentication(t)
	refreshing := ws.DefaultConfig()
	refreshing.AuthRefreshInterval = 150 * time.Millisecond
	refreshing.OperationTimeout = time.Second
	users := ws.OwnedRooms[AccountChannel]("accounts", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.users, (Account{}).FoundryReference())
	admins := ws.OwnedRooms[AdminChannel]("admins", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.admins, (Account{}).FoundryReference())
	var handled atomic.Int32
	saved := make(chan context.Context, 1)
	in := ws.DefineIncoming(users, "edit", echoContract()).Authorize(func(ctx context.Context, message ws.MessageContext[int64, Account], payload Echo) error {
		if payload.Text != "allowed" {
			return auth.Forbidden
		}
		return nil
	})
	f := serve(t, registry(t, ws.Register(users, in.Handle(func(ctx context.Context, message ws.MessageContext[int64, Account], payload Echo) error {
		if message.Subject.ID != 7 || attribution.FromContext(ctx).Guard() != "users" {
			return ws.OperationFailed
		}
		handled.Add(1)
		saved <- ctx
		return nil
	})), ws.Register(admins)), a.transport, refreshing)
	peer := f.dial(t, userHeaders())
	for _, request := range []ws.Request{{Action: ws.Subscribe, ID: "whole", Channel: "accounts"}, {Action: ws.Subscribe, ID: "wrong-room", Channel: "accounts", Room: room("8")}} {
		send(t, peer, request)
		if r := receive(t, peer, ws.ErrorResponse); r.Code != ws.Forbidden {
			t.Fatal("identity ownership bypassed")
		}
	}
	send(t, peer, ws.Request{Action: ws.Subscribe, ID: "other-guard", Channel: "admins", Room: room("7")})
	if r := receive(t, peer, ws.ErrorResponse); r.Code != ws.Unauthenticated {
		t.Fatal("same ID crossed guard namespace")
	}
	subscribe(t, peer, "own", users.ID(), room("7"))
	send(t, peer, ws.Request{Action: ws.Message, ID: "denied-event", Channel: users.ID(), Room: room("7"), Event: "edit", Payload: json.RawMessage(`{"text":"denied"}`)})
	if receive(t, peer, ws.ErrorResponse).Code != ws.Forbidden {
		t.Fatal("event policy not applied")
	}
	send(t, peer, ws.Request{Action: ws.Message, ID: "allowed-event", Channel: users.ID(), Room: room("7"), Event: "edit", Payload: json.RawMessage(`{"text":"allowed"}`)})
	receive(t, peer, ws.Acknowledged)
	retained := <-saved
	if _, err := a.users.Require(retained); err == nil {
		t.Fatal("completed operation kept an authentication scope")
	}
	// Messages reuse the connection's authorization freshness window instead of
	// loading the subject again for every frame.
	before := a.loads.Load()
	send(t, peer, ws.Request{Action: ws.Message, ID: "cached-event", Channel: users.ID(), Room: room("7"), Event: "edit", Payload: json.RawMessage(`{"text":"allowed"}`)})
	receive(t, peer, ws.Acknowledged)
	<-saved
	if a.loads.Load() != before || handled.Load() != 2 {
		t.Fatal("message repeated credential lookup inside the freshness window")
	}
	// The periodic refresh remains the revocation backstop: it disconnects the
	// revoked subject within one refresh interval.
	a.disabled.Store(true)
	deadline := time.Now().Add(3 * time.Second)
	for f.hub.Snapshot().Connections != 0 {
		if time.Now().After(deadline) {
			t.Fatal("revoked subject was not disconnected by refresh")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if handled.Load() != 2 {
		t.Fatal("revoked subject invoked handler")
	}
}

func TestPresenceOnlyExportsSafeDTOAndCountsConnections(t *testing.T) {
	a := authentication(t)
	var joins, leaves atomic.Int32
	base := ws.Private[PresenceOwner]("presence", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.users, func(context.Context, Account, ws.Target[int64]) error { return nil })
	base = base.WithHooks(ws.Hooks[int64, Account]{Joined: func(context.Context, ws.MessageContext[int64, Account]) error { joins.Add(1); return nil }, Left: func(ctx context.Context, message ws.LeaveContext[int64]) error {
		if message.Subject.Guard != "users" || attribution.FromContext(ctx).Guard() != "users" {
			return ws.OperationFailed
		}
		leaves.Add(1)
		return nil
	}})
	presence := ws.WithPresence(base, textContract[SafeMember]("display"), func(_ context.Context, subject Account) (SafeMember, error) {
		return SafeMember{Display: subject.Display}, nil
	})
	f := serve(t, registry(t, ws.Register(presence.Channel())), a.transport, ws.DefaultConfig())
	first, second := f.dial(t, userHeaders()), f.dial(t, userHeaders())
	initial := subscribe(t, first, "one", base.ID(), room("9"))
	if len(initial.Members) != 1 || initial.Members[0].Connections != 1 {
		t.Fatal("missing initial member")
	}
	joined := receive(t, first, ws.PresenceJoined)
	if joined.Member == nil || strings.Contains(string(joined.Member.Data), "private") || string(joined.Member.Data) != `{"display":"safe-name"}` {
		t.Fatal("presence leaked a model field")
	}
	secondJoin := subscribe(t, second, "two", base.ID(), room("9"))
	if len(secondJoin.Members) != 1 || secondJoin.Members[0].Connections != 2 || secondJoin.Members[0].ID != initial.Members[0].ID {
		t.Fatal("tabs became different subjects")
	}
	receive(t, first, ws.PresenceUpdated)
	receive(t, second, ws.PresenceUpdated)
	members, err := presence.Members(t.Context(), f.hub, ws.Room(int64(9)))
	if err != nil || len(members) != 1 || members[0].Connections != 2 || members[0].Data.Display != "safe-name" {
		t.Fatal("typed member inspection failed")
	}
	other := ws.WithPresence(base, textContract[SafeMember]("display"), func(context.Context, Account) (SafeMember, error) { return SafeMember{}, nil })
	if _, err := other.Members(t.Context(), f.hub, ws.Room(int64(9))); err == nil {
		t.Fatal("unregistered presence declaration inspected members")
	}
	send(t, first, ws.Request{Action: ws.Unsubscribe, ID: "leave", Channel: base.ID(), Room: room("9")})
	receive(t, first, ws.Unsubscribed)
	if event := receive(t, second, ws.PresenceUpdated); event.Member.Connections != 1 {
		t.Fatal("first tab removed the subject")
	}
	second.Close()
	waitFor(t, func() bool { return f.hub.Snapshot().Subscriptions == 0 && leaves.Load() == 2 })
	members, err = presence.Members(t.Context(), f.hub, ws.Room(int64(9)))
	if err != nil || len(members) != 0 || joins.Load() != 2 {
		t.Fatal("presence/cleanup ownership leaked")
	}
}
