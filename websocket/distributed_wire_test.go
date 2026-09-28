package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestPublicationWireBoundsAndStrictFields(t *testing.T) {
	id, err := model.NewID[Publication]()
	if err != nil {
		t.Fatal(err)
	}
	good, err := json.Marshal(Response{Version: 1, Type: EventResponse, Channel: "chat", Event: "updated", MessageID: id, Payload: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePublication(good, 4096); err != nil {
		t.Fatal(err)
	}
	for _, maximum := range []int{0, -1, 1<<20 + 1} {
		if _, err := DecodePublication(good, maximum); err == nil {
			t.Fatal("invalid bound accepted")
		}
	}
	for _, mutate := range []func(map[string]any){func(m map[string]any) { m["V"] = m["v"]; delete(m, "v") }, func(m map[string]any) { m["replayed"] = true }, func(m map[string]any) { m["room"] = "" }, func(m map[string]any) { delete(m, "payload") }, func(m map[string]any) { m["message_id"] = "invalid" }, func(m map[string]any) { m["code"] = "forbidden" }} {
		var m map[string]any
		if err := json.Unmarshal(good, &m); err != nil {
			t.Fatal(err)
		}
		mutate(m)
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodePublication(data, 4096); err == nil {
			t.Fatal("malformed publication accepted")
		}
	}
}
func FuzzDecodePublication(f *testing.F) {
	f.Add([]byte(`{"v":1,"type":"event","channel":"chat","event":"updated","message_id":"01990000-0000-7000-8000-000000000001","payload":{}}`))
	f.Add([]byte(`{"v":1,"v":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			return
		}
		response, err := DecodePublication(data, 65536)
		if err == nil && (response.MessageID.IsZero() || response.Type != EventResponse || len(response.Payload) == 0) {
			t.Fatal("invalid successful decode")
		}
	})
}
func TestPendingBuffersAndHistoryAreBoundedAndReleased(t *testing.T) {
	type owner struct{}
	channel := Public[owner]("bounded", DefineRooms(foundryhttp.IntegerPath[int64]())).WithReplay(ReplayConfig{Messages: 2, Bytes: 1024, TTL: time.Minute})
	r, err := NewRegistry(Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.DeduplicationEntries = 2
	config.OutboundQueue = 3
	h, err := New(r, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop(context.Background())
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	c := &connectionState{hub: h, ctx: ctx, cancel: cancel, pending: make(map[subscriptionKey]*pendingSubscription), subscriptions: make(map[subscriptionKey]*subscriptionState), outbound: make(chan []byte, 3)}
	key := subscriptionKey{channel: channel.ID(), room: "1", hasRoom: true}
	sub := &subscriptionState{key: key, channel: r.channels[channel.ID()]}
	h.mu.Lock()
	defer h.mu.Unlock()
	c.reservePendingLocked(sub)
	for i := 0; i < 4; i++ {
		id, err := model.NewID[Publication]()
		if err != nil {
			t.Fatal(err)
		}
		response := Response{Version: 1, Type: EventResponse, Channel: channel.ID(), MessageID: id}
		c.bufferPendingLocked(c.pending[key], response, []byte(`{}`))
	}
	if len(c.pending[key].frames) > config.OutboundQueue || c.ctx.Err() == nil {
		t.Fatal("pending admission queue grew without disconnect")
	}
	c.dropPendingLocked(key)
	if len(c.pending) != 0 {
		t.Fatal("failed admission retained pending state")
	}
}
