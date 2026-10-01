package application_test

import (
	"context"
	"io"
	stdhttp "net/http"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type ticketMember struct{ ID int64 }

func (m ticketMember) FoundryReference() model.Reference[ticketMember, int64] {
	return model.NewReference[ticketMember]("ticket_members", m.ID, codec.Signed[int64]())
}
func (m ticketMember) FoundryIdentity() (model.Identity, error) {
	return m.FoundryReference().Identity()
}

type ticketInbox struct{}

func TestRealtimeTicketsAuthenticateBrowserSocketsUntilRevocation(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	s := realtimeSettings()
	s.Realtime.Enabled, s.Realtime.Shared = true, true
	s.Realtime.Config.AuthRefreshInterval = 100 * time.Millisecond
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	connection.Primary.Schema = schema
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	s.Features.Auth.Tokens.Enabled, s.Features.Auth.Tokens.Schema = true, schema
	provider := auth.DefineProvider("ticket.members", (ticketMember{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[ticketMember], error) {
		return value.Set(ticketMember{ID: id}), nil
	}, func(context.Context, ticketMember) (bool, error) { return true, nil })
	allowed, err := auth.NewAccessScopes[ticketMember]()
	if err != nil {
		t.Fatal(err)
	}
	var (
		once  sync.Once
		guard application.TokenGuard[ticketMember, int64]
	)
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Realtime(func(services application.Services) (application.RealtimeDeclarations, error) {
		var err error
		once.Do(func() { guard, err = application.NewTokenGuard(services, "", provider, "member.bearer", allowed) })
		if err != nil {
			return application.RealtimeDeclarations{}, err
		}
		inbox := websocket.OwnedRooms[ticketInbox]("inbox", websocket.DefineRooms(http.IntegerPath[int64]()), guard.Tokens.Guard(), (ticketMember{}).FoundryReference())
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(inbox)}, Authentication: guard.Authentication, Tickets: []websocket.TicketRedeemer{guard.Tokens}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	up, err := dbcommand.Parse([]string{"migrate", "up"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RunDatabaseCommand(t.Context(), up, application.DatabaseCommandResources{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	address, err := app.HTTPReady(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	url, origin := "ws://"+address+s.Realtime.Path, stdhttp.Header{"Origin": []string{"http://" + address}}
	member := ticketMember{ID: 41}
	proof, err := auth.NewProof(member.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := guard.Tokens.Issue(t.Context(), proof, token.IssueOptions[ticketMember]{Name: "browser", Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	// The issuance endpoint runs behind the bearer guard; its scope supplies the family.
	credentials, err := auth.NewCredentials(auth.Credential{Name: "member.bearer", Secret: issued.AccessSecret()})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := guard.Authentication.Registry().NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := guard.Tokens.IssueTicket(scope.Context())
	scope.Close()
	if err != nil {
		t.Fatal(err)
	}
	dial, dialCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer dialCancel()
	peer, err := client.DialTicket(dial, url, origin, 64<<10, ticket.Secret())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	join := func(c *client.Client, id websocket.RequestID) {
		t.Helper()
		room, _ := http.IntegerPath[int64]().Format(member.ID)
		if err := c.Send(dial, websocket.Request{Version: websocket.ProtocolVersion, Action: websocket.Subscribe, ID: id, Channel: "inbox", Room: &room}); err != nil {
			t.Fatal(err)
		}
		if reply, err := c.Receive(dial); err != nil || reply.Type != websocket.Subscribed {
			t.Fatal("owned room was not joined", reply, err)
		}
	}
	join(peer, "ticket")
	if _, err := client.DialTicket(dial, url, origin, 64<<10, ticket.Secret()); err == nil {
		t.Fatal("replayed ticket accepted")
	}
	native, err := client.Dial(dial, url, stdhttp.Header{"Origin": origin["Origin"], "Authorization": []string{"Bearer " + issued.AccessSecret().Reveal()}}, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	join(native, "native")
	if _, err := guard.Tokens.RevokeAll(t.Context(), member.FoundryReference()); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := peer.Receive(dial); err != nil {
			if dial.Err() != nil {
				t.Fatal("revoking the family left the ticket socket open")
			}
			break
		}
	}
	if _, err := client.DialTicket(dial, url, origin, 64<<10, secret.New("unknown")); err == nil {
		t.Fatal("unknown ticket accepted")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("realtime did not drain")
	}
}
