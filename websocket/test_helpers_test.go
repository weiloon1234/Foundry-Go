package websocket_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

type Chat struct{}
type Echo struct {
	Text string `json:"text"`
}
type SafeMember struct {
	Display string `json:"display"`
}

func textContract[T any](name string) contract.JSON[T] {
	typ := reflect.TypeFor[T]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[T](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: name, Type: "text", Required: true}}}, {ID: "text", Kind: contract.StringKind}}})
}
func echoContract() contract.JSON[Echo] { return textContract[Echo]("text") }
func publicChannel() ws.Channel[Chat, int64, ws.Anonymous] {
	return ws.Public[Chat]("chat", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
}
func registry(t *testing.T, registrations ...ws.Registration) *ws.Registry {
	t.Helper()
	r, err := ws.NewRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type serverFixture struct {
	hub    *ws.Hub
	server *httptest.Server
}

func serve(t *testing.T, r *ws.Registry, authentication *foundryhttp.Authentication, config ws.Config) serverFixture {
	return serveWith(t, r, authentication, config, nil)
}
func serveWith(t *testing.T, r *ws.Registry, authentication *foundryhttp.Authentication, config ws.Config, wrap func(http.Handler) http.Handler, opts ...ws.Option) serverFixture {
	t.Helper()
	hub, err := ws.New(r, authentication, config, opts...)
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = hub
	if wrap != nil {
		handler = wrap(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := hub.Stop(ctx); err != nil {
			t.Error(err)
			server.CloseClientConnections()
			return
		}
		server.Close()
	})
	return serverFixture{hub, server}
}
func (f serverFixture) dial(t *testing.T, headers http.Header) *client.Client {
	t.Helper()
	if headers == nil {
		headers = make(http.Header)
	} else {
		headers = headers.Clone()
	}
	headers.Set("Origin", f.server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", headers, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func send(t *testing.T, c *client.Client, r ws.Request) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r.Version = ws.ProtocolVersion
	if err := c.Send(ctx, r); err != nil {
		t.Fatal(err)
	}
}
func receive(t *testing.T, c *client.Client, kind ws.ResponseType) ws.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r, err := c.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != kind {
		t.Fatalf("expected %s, got %s (%s)", kind, r.Type, r.Code)
	}
	return r
}
func room(text string) *string { return &text }
func subscribe(t *testing.T, c *client.Client, id ws.RequestID, channel ws.ChannelID, target *string) ws.Response {
	t.Helper()
	send(t, c, ws.Request{Action: ws.Subscribe, ID: id, Channel: channel, Room: target})
	return receive(t, c, ws.Subscribed)
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("WebSocket condition did not become true")
}
