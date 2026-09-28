package events_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	eventstest "github.com/weiloon1234/Foundry-Go/testkit/events"
)

type Notice struct {
	Values map[string]string `json:"values"`
}

func TestRecorderUsesProductionDispatchAndOwnedSnapshots(t *testing.T) {
	recorder, err := eventstest.New[Notice](2)
	if err != nil {
		t.Fatal(err)
	}
	topic := events.Define[Notice]("test.notice", 1)
	declaration, err := topic.Declare(recorder.Listener("test.record"))
	if err != nil {
		t.Fatal(err)
	}
	bus := eventstest.Start(t, declaration)
	input := Notice{Values: map[string]string{"key": "private-original"}}
	if err := topic.Dispatch(t.Context(), bus, input); err != nil {
		t.Fatal(err)
	}
	input.Values["key"] = "mutated"
	items, err := recorder.Payloads(t.Context())
	if err != nil || items[0].Values["key"] != "private-original" {
		t.Fatal("record retained caller memory", err)
	}
	items[0].Values["key"] = "changed returned snapshot"
	items, err = recorder.Payloads(t.Context())
	if err != nil || items[0].Values["key"] != "private-original" {
		t.Fatal("record exposed internal memory", err)
	}
	if text := fmt.Sprintf("%#v", recorder); text != "event recorder" {
		t.Fatal("record diagnostic exposes private data")
	}
	undeclared := events.Define[Notice]("test.undeclared", 1)
	if err := undeclared.Dispatch(t.Context(), bus, input); err == nil {
		t.Fatal("fake bypassed declaration checks")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := topic.Dispatch(ctx, bus, input); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := recorder.Payloads(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := topic.Dispatch(t.Context(), bus, input); err != nil {
		t.Fatal(err)
	}
	if err := topic.Dispatch(t.Context(), bus, input); !errors.Is(err, fault.Invalid) {
		t.Fatal("recorder exceeded capacity", err)
	}
	eventstest.AssertCount(t, recorder, 2)
}

func TestRecordersRemainIndependentAndConcurrent(t *testing.T) {
	one, _ := eventstest.New[Notice](32)
	two, _ := eventstest.New[Notice](32)
	topic := events.Define[Notice]("test.notice", 1)
	first, _ := topic.Declare(one.Listener("first"))
	second, _ := topic.Declare(two.Listener("second"))
	firstBus := eventstest.Start(t, first)
	secondBus := eventstest.Start(t, second)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := topic.Dispatch(t.Context(), firstBus, Notice{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	eventstest.AssertCount(t, one, 20)
	eventstest.AssertCount(t, two, 0)
	if err := topic.Dispatch(t.Context(), secondBus, Notice{}); err != nil {
		t.Fatal(err)
	}
	eventstest.AssertCount(t, two, 1)
	for _, capacity := range []int{0, eventstest.MaxEvents + 1} {
		if _, err := eventstest.New[Notice](capacity); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
