package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type encodingFailure string

func (v encodingFailure) MarshalJSON() ([]byte, error) {
	switch v {
	case "panic":
		panic("private encoder data")
	case "exit":
		runtime.Goexit()
	case "error":
		return nil, errors.New("private encoder error")
	}
	return json.Marshal(string(v))
}
func TestCaptureContainsCodecFailuresBeforeAnyListener(t *testing.T) {
	topic := events.Define[encodingFailure]("test.encoding", 1)
	calls := 0
	config := events.DefaultConfig()
	config.MaxInFlight = 1
	bus := startedBus(t, config, declaration(t, topic, events.Listen("record", func(context.Context, encodingFailure) error { calls++; return nil })))
	for _, failure := range []encodingFailure{"panic", "exit", "error"} {
		err := topic.Dispatch(t.Context(), bus, failure)
		expected := error(fault.Panicked)
		if failure == "error" {
			expected = fault.Invalid
		}
		if !errors.Is(err, expected) || strings.Contains(err.Error(), "private") || calls != 0 {
			t.Fatal("payload codec failure escaped or exposed data", failure, err)
		}
	}
	if err := topic.Dispatch(t.Context(), bus, "valid"); err != nil || calls != 1 {
		t.Fatal("payload failure leaked dispatch admission", err)
	}
}

var decodeFailure atomic.Int32

type decodingPayload struct {
	Fail bool `json:"fail"`
}

func (p *decodingPayload) UnmarshalJSON(data []byte) error {
	if decodeFailure.Load() == 1 {
		panic("private decoder data")
	}
	if decodeFailure.Load() == 2 {
		runtime.Goexit()
	}
	if decodeFailure.Load() == 3 {
		return errors.New("private decoder error")
	}
	type plain decodingPayload
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = decodingPayload(decoded)
	return nil
}
func TestListenerDecodeFailuresDoNotRunLaterListenersOrLeakAdmission(t *testing.T) {
	// Deliberately fault a custom codec after capture, at a later listener decode.
	// This global is only a sequential test injection; production codecs are pure.
	defer decodeFailure.Store(0)
	topic := events.Define[decodingPayload]("test.decoding", 1)
	for _, failure := range []int32{1, 2, 3} {
		decodeFailure.Store(0)
		later := false
		config := events.DefaultConfig()
		config.MaxInFlight = 1
		bus := startedBus(t, config, declaration(t, topic,
			events.Listen("inject", func(_ context.Context, input decodingPayload) error {
				if input.Fail {
					decodeFailure.Store(failure)
				}
				return nil
			}),
			events.Listen("later", func(context.Context, decodingPayload) error { later = true; return nil })))
		err := topic.Dispatch(t.Context(), bus, decodingPayload{Fail: true})
		expected := error(fault.Panicked)
		if failure == 3 {
			expected = fault.Invalid
		}
		if !errors.Is(err, expected) || strings.Contains(err.Error(), "private") || later {
			t.Fatal("later payload decode escaped or ran its handler", err)
		}
		decodeFailure.Store(0)
		if err := topic.Dispatch(t.Context(), bus, decodingPayload{}); err != nil || !later {
			t.Fatal("decoder failure leaked admission", err)
		}
	}
}

type blockedEncoding struct {
	Value   int           `json:"value"`
	Entered chan struct{} `json:"-"`
	Release chan struct{} `json:"-"`
}

func (v blockedEncoding) MarshalJSON() ([]byte, error) {
	close(v.Entered)
	<-v.Release
	return json.Marshal(struct {
		Value int `json:"value"`
	}{v.Value})
}
func TestCancellationWaitsForActualEncoderExit(t *testing.T) {
	topic := events.Define[blockedEncoding]("test.slow_encoding", 1)
	called := false
	bus := startedBus(t, events.DefaultConfig(), declaration(t, topic, events.Listen("record", func(context.Context, blockedEncoding) error { called = true; return nil })))
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- topic.Dispatch(ctx, bus, blockedEncoding{Value: 7, Entered: entered, Release: release})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("encoder did not start")
	}
	cancel()
	select {
	case <-result:
		t.Fatal("dispatch abandoned an active encoder")
	default:
	}
	released.Do(func() { close(release) })
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || called {
			t.Fatal("canceled capture ran a listener", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("encoder exit did not release dispatch")
	}
}
