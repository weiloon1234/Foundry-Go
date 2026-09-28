package websocket_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestRecordedProtocolConversationRemainsCompatible(t *testing.T) {
	data, err := os.ReadFile("testdata/protocol-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var frames []struct {
		Request  json.RawMessage `json:"request"`
		Response ws.Response     `json:"response"`
	}
	if err := json.Unmarshal(data, &frames); err != nil {
		t.Fatal(err)
	}
	channel := publicChannel()
	incoming := ws.DefineIncoming(channel, "send", echoContract())
	f := serve(t, registry(t, ws.Register(channel, incoming.Handle(func(context.Context, ws.MessageContext[int64, ws.Anonymous], Echo) error { return nil }))), nil, ws.DefaultConfig())
	peer := f.dial(t, nil)
	for _, frame := range frames {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		err := peer.SendText(ctx, frame.Request)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		got := receive(t, peer, frame.Response.Type)
		if !reflect.DeepEqual(got, frame.Response) {
			t.Fatal("recorded version-one response changed")
		}
	}
}
