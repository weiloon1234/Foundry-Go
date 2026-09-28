package websocket_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestConnectionCopiesOnlyRequestMetadata(t *testing.T) {
	type privateKey struct{}
	channel := publicChannel()
	var valid atomic.Bool
	incoming := ws.DefineIncoming(channel, "send", echoContract())
	registration := ws.Register(channel, incoming.Handle(func(ctx context.Context, _ ws.MessageContext[int64, ws.Anonymous], _ Echo) error {
		origin := attribution.FromContext(ctx)
		_, modelPresent := origin.Model()
		valid.Store(ctx.Value(privateKey{}) == nil && origin.System() == "" && !modelPresent && origin.Request().ID == "handshake-correlation")
		return nil
	}))
	f := serveWith(t, registry(t, registration), nil, ws.DefaultConfig(), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin, err := (attribution.Origin{}).WithSystem("inherited-authority")
			if err != nil {
				t.Error(err)
				return
			}
			origin, err = origin.WithRequest(attribution.Request{ID: "handshake-correlation"})
			if err != nil {
				t.Error(err)
				return
			}
			ctx, err := attribution.WithContext(r.Context(), origin)
			if err != nil {
				t.Error(err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, privateKey{}, "private request value")))
		})
	})
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), nil)
	send(t, peer, ws.Request{Action: ws.Message, ID: "message", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"ok"}`)})
	receive(t, peer, ws.Acknowledged)
	if !valid.Load() {
		t.Fatal("connection retained request values/authority or lost correlation metadata")
	}
}
