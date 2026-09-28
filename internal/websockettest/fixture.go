// Package websockettest exercises the public distributed runtime against an
// externally supplied real authority. Redis environment ownership stays in redis.
package websockettest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/value"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

type RoomOwner struct{}
type PresenceOwner struct{}
type Payload struct {
	Text string `json:"text"`
}
type Member struct {
	Text string `json:"text"`
}
type Account struct {
	ID      int64
	Private string
}

func (a Account) FoundryReference() model.Reference[Account, int64] {
	return model.NewReference[Account]("distributed_accounts", a.ID, codec.Signed[int64]())
}
func (a Account) FoundryIdentity() (model.Identity, error) { return a.FoundryReference().Identity() }
func textContract[T any]() contract.JSON[T] {
	typ := reflect.TypeFor[T]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[T](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "text", Type: "text", Required: true}}}, {ID: "text", Kind: contract.StringKind}}})
}
func authenticated(t *testing.T) (*foundryhttp.Authentication, auth.Guard[Account]) {
	t.Helper()
	provider := auth.DefineProvider("distributed.accounts", (Account{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[Account], error) {
		return value.Set(Account{id, "never-export"}), nil
	}, func(context.Context, Account) (bool, error) { return true, nil })
	proof, err := auth.NewProof(Account{ID: 7}.FoundryReference(), auth.Authenticated)
	must(t, err)
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[Account, int64]], error) {
		if token.Reveal() != "test-user" {
			return value.Optional[auth.Proof[Account, int64]]{}, nil
		}
		return value.Set(proof), nil
	})
	guard := auth.DefineGuard("users", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	must(t, err)
	adapter, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	must(t, err)
	return adapter, guard
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("distributed condition did not become true")
}
func room(s string) *string { return &s }
func registry(t *testing.T, items ...ws.Registration) *ws.Registry {
	t.Helper()
	r, err := ws.NewRegistry(items...)
	must(t, err)
	return r
}
func localConfig() ws.Config {
	c := ws.DefaultConfig()
	c.MaxConnections = 16
	c.OutboundQueue = 64
	return c
}

type server struct {
	hub  *ws.Hub
	http *httptest.Server
}

func serve(t *testing.T, r *ws.Registry, a *foundryhttp.Authentication, b ws.ClusterBackend, cluster ws.ClusterConfig) *server {
	t.Helper()
	hub, err := ws.NewDistributed(r, a, localConfig(), b, cluster)
	must(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	must(t, hub.Start(ctx))
	s := &server{hub, httptest.NewServer(hub)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		err := hub.Stop(ctx)
		if err != nil && !hub.Snapshot().Degraded {
			t.Error(err)
		}
		s.http.CloseClientConnections()
		s.http.Close()
	})
	return s
}

type peer struct {
	client *client.Client
	frames chan ws.Response
	done   chan struct{}
	cancel context.CancelFunc
}

func (s *server) dial(t *testing.T, private bool) *peer {
	t.Helper()
	headers := http.Header{"Origin": []string{s.http.URL}}
	if private {
		headers.Set("Authorization", "Bearer test-user")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	socket, err := client.Dial(ctx, "ws"+strings.TrimPrefix(s.http.URL, "http"), headers, 64<<10)
	must(t, err)
	read, stop := context.WithCancel(context.Background())
	p := &peer{socket, make(chan ws.Response, 128), make(chan struct{}), stop}
	go func() {
		defer close(p.done)
		for {
			frame, err := socket.Receive(read)
			if err != nil {
				return
			}
			select {
			case p.frames <- frame:
			case <-read.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		p.close()
		select {
		case <-p.done:
		case <-time.After(time.Second):
			t.Error("client reader leaked")
		}
	})
	return p
}
func (p *peer) close() { p.cancel(); _ = p.client.Close() }
func (p *peer) send(t *testing.T, r ws.Request) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r.Version = ws.ProtocolVersion
	must(t, p.client.Send(ctx, r))
}
func (p *peer) next(t *testing.T, kind ws.ResponseType) ws.Response {
	t.Helper()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-p.frames:
			if r.Type == ws.PresenceJoined || r.Type == ws.PresenceUpdated || r.Type == ws.PresenceLeft {
				if r.Type != kind {
					continue
				}
			}
			if r.Type != kind {
				t.Fatalf("expected %s, got %s (%s)", kind, r.Type, r.Code)
			}
			return r
		case <-p.done:
			t.Fatal("connection closed before expected frame")
		case <-timer.C:
			t.Fatal("frame deadline exceeded")
		}
	}
}
func (p *peer) subscribe(t *testing.T, id ws.RequestID, ch ws.ChannelID, r *string, replay *int) ws.Response {
	t.Helper()
	p.send(t, ws.Request{Action: ws.Subscribe, ID: id, Channel: ch, Room: r, Replay: replay})
	return p.next(t, ws.Subscribed)
}
func (p *peer) barrier(t *testing.T, ch ws.ChannelID, r *string) {
	t.Helper()
	p.send(t, ws.Request{Action: ws.Unsubscribe, ID: "barrier", Channel: ch, Room: r})
	p.next(t, ws.Unsubscribed)
}
func payload(text string) json.RawMessage { data, _ := json.Marshal(Payload{text}); return data }
