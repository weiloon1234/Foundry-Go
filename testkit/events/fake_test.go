package events_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/events"
	eventtest "github.com/weiloon1234/Foundry-Go/testkit/events"
)

type Shipped struct {
	Order string `json:"order"`
}

type failures struct {
	testing.TB
	messages []string
}

func (f *failures) Errorf(format string, args ...any) {
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

func TestFakeSuppressesListenersAndAssertsTypedPayloads(t *testing.T) {
	topic := events.Define[Shipped]("orders.shipped", 1)
	var ran atomic.Int32
	declaration, err := topic.Declare(events.Listen("notify", func(context.Context, Shipped) error { ran.Add(1); return nil }))
	if err != nil {
		t.Fatal(err)
	}
	bus := eventtest.Start(t, declaration)
	t.Run("faked", func(t *testing.T) {
		fake := eventtest.NewFake(t, bus)
		for _, order := range []string{"A-1", "A-2"} {
			if err := topic.Dispatch(t.Context(), bus, Shipped{Order: order}); err != nil {
				t.Fatal(err)
			}
		}
		if ran.Load() != 0 {
			t.Fatal("faked bus ran a real listener")
		}
		second := func(s Shipped) bool { return s.Order == "A-2" }
		eventtest.AssertDispatched(t, fake, topic, second)
		eventtest.AssertDispatchedCount(t, fake, topic, nil, 2)
		eventtest.AssertNotDispatched(t, fake, topic, func(s Shipped) bool { return s.Order == "B-1" })
		recorded := &failures{TB: t}
		eventtest.AssertNotDispatched(recorded, fake, topic, second)
		if len(recorded.messages) != 1 || strings.Contains(recorded.messages[0], "A-2") {
			t.Fatal("assertion failure missing or exposed a payload", recorded.messages)
		}
	})
	// Cleanup restored the bus: listeners run again.
	if err := topic.Dispatch(t.Context(), bus, Shipped{Order: "A-3"}); err != nil || ran.Load() != 1 {
		t.Fatal("fake was not removed", err, ran.Load())
	}
}
