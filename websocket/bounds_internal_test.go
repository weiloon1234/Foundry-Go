package websocket

import (
	"context"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestSlowConsumerDisconnectsWithoutBlockingPublisher(t *testing.T) {
	type owner struct{}
	channel := Public[owner]("bounds", DefineRooms(foundryhttp.IntegerPath[int64]()))
	r, err := NewRegistry(Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(r, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop(context.Background())
	id, err := model.NewID[Connection]()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	c := &connectionState{hub: h, id: id, ctx: ctx, cancel: cancel, outbound: make(chan []byte, 1)}
	h.mu.Lock()
	first := c.enqueueLocked([]byte("first"))
	second := c.enqueueLocked([]byte("second"))
	h.mu.Unlock()
	if !first || second || ctx.Err() == nil || h.Snapshot().SlowConsumers != 1 {
		t.Fatal("slow peer did not release transport promptly")
	}
}

func TestConfigurationBoundsAggregateQueuesAndPresenceFrames(t *testing.T) {
	for _, mutate := range []func(*Config){func(c *Config) { c.InboundQueue = 0 }, func(c *Config) { c.MaxConnections = 65536; c.OutboundQueue = 1024 }, func(c *Config) { c.MaxPresenceMembers = 4096 }, func(c *Config) { c.Payload.Bytes = c.MaxFrameBytes }, func(c *Config) { c.AdditionalOrigins = []foundryhttp.Origin{"null"} }, func(c *Config) {
		c.AdditionalOrigins = []foundryhttp.Origin{"https://a.example", "https://A.example:443"}
	}} {
		c := DefaultConfig()
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("invalid resource/origin policy accepted")
		}
	}
}
