package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type NotificationRoom struct{}

func TestNotificationRealtimePrivateOwnershipAndRetryDedup(t *testing.T) {
	a := newAuthority(t)
	const channelID websocket.ChannelID = "member.notifications"
	realtime := DefineRealtime[NotificationRoom](a.recipient, channelID, websocket.DefineRooms(foundryhttp.IntegerPath[int64]()), "notification", textSchema[InboxData]())
	registry, err := websocket.NewRegistry(realtime.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(a.registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	hub, err := websocket.New(registry, transport, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hub)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hub.Stop(ctx); err != nil {
			t.Error(err)
			server.CloseClientConnections()
		}
		server.Close()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	headers := http.Header{"Origin": {server.URL}, "Authorization": {"Bearer 1"}}
	peer, err := client.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", headers, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	request := func(action websocket.Action, id websocket.RequestID, room *string, event websocket.EventID, payload json.RawMessage) websocket.Response {
		t.Helper()
		if err := peer.Send(ctx, websocket.Request{Version: websocket.ProtocolVersion, Action: action, ID: id, Channel: channelID, Room: room, Event: event, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		response, err := peer.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	other, own := "2", "1"
	if result := request(websocket.Subscribe, "wide", nil, "", nil); result.Code != websocket.Forbidden {
		t.Fatal("wide subscription allowed")
	}
	if result := request(websocket.Subscribe, "foreign", &other, "", nil); result.Code != websocket.Forbidden {
		t.Fatal("foreign recipient room allowed")
	}
	if result := request(websocket.Subscribe, "own", &own, "", nil); result.Type != websocket.Subscribed {
		t.Fatal("own room rejected")
	}
	realtimeChannel := RealtimeChannel("realtime", realtime, hub, func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	})
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, databaseChannel().Channel(), realtimeChannel)
	m, _ := fixture(t, b.Registration())
	pending := captureNotification(t, b, 1, "visible")
	if _, err := pending.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	message, err := peer.Receive(ctx)
	if err != nil || message.Type != websocket.EventResponse {
		t.Fatal("missing private notification", err)
	}
	var body RealtimeMessage[InboxData]
	if err := json.Unmarshal(message.Payload, &body); err != nil || body.ID.Bytes() != pending.ID().Bytes() || body.Data.Text != "visible" {
		t.Fatal("notification envelope changed", err)
	}
	if _, err := pending.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	// A protocol reply on the same FIFO is a deterministic barrier: an accidental
	// second broadcast would arrive before this wrong-direction response.
	if result := request(websocket.Message, "barrier", &own, "notification", message.Payload); result.Type != websocket.ErrorResponse || result.Code != websocket.WrongDirection {
		t.Fatal("notification rebroadcast or client publishing permitted")
	}
	a.set(Member{ID: 1, Enabled: false, Allowed: true})
	if result := request(websocket.Subscribe, "revoked", &own, "", nil); result.Code != websocket.Unauthenticated {
		t.Fatal("revoked recipient subscribed")
	}
}

// A realtime publication that provably never started (the hub is stopped) is
// retried; it is never reported as uncertain.
func TestNotificationRealtimeRetriesAPublicationThatNeverStarted(t *testing.T) {
	a := newAuthority(t)
	realtime := DefineRealtime[NotificationRoom](a.recipient, "member.stopped", websocket.DefineRooms(foundryhttp.IntegerPath[int64]()), "notification", textSchema[InboxData]())
	registry, err := websocket.NewRegistry(realtime.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(a.registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	hub, err := websocket.New(registry, transport, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	stop, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := hub.Stop(stop); err != nil {
		t.Fatal(err)
	}
	channel := RealtimeChannel("realtime", realtime, hub, func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	})
	b := Bind(Define("stopped.notice", 1, textSchema[Input]()), a.recipient, channel)
	m, _ := fixture(t, b.Registration())
	report, _ := captureNotification(t, b, 1, "later").Send(t.Context(), m)
	if len(report.Channels) != 1 || report.Channels[0].State != Prepared {
		t.Fatalf("unstarted publication was not left for retry: %+v", report.Channels)
	}
}
