package websocket

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

type privateSocketDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateSocketDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private socket diagnostic")
}
func (e *privateSocketDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestWebSocketCodeDiagnosticsSurviveContainment(t *testing.T) {
	for _, code := range []Code{Malformed, UnsupportedVersion, UnknownChannel, UnknownEvent,
		WrongDirection, Unauthenticated, Forbidden, NotSubscribed, AlreadySubscribed,
		InvalidPayload, CapacityExceeded, OperationFailed, OperationTimedOut, Stopping,
		RateLimited, Unavailable, "private code"} {
		t.Run(string(code), func(t *testing.T) {
			original := codeFailure(code)
			result := callback.Isolated("socket", func() error { return original })
			want := "socket: " + string(code)
			if code == "private code" {
				want = "socket failed"
			}
			var retained Code
			if result.Error() != want || !errors.Is(result, original) || !errors.Is(result, code) || !errors.As(result, &retained) || retained != code {
				t.Fatal("socket classification lost its diagnostic or identity")
			}
		})
	}
}

func TestWebSocketPipelineDiagnosticsSurviveContainment(t *testing.T) {
	connection, err := model.NewIDAt[Connection](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, message  string
		classification error
		notPublished   bool
		run            func() error
	}{
		{"decoder", "malformed", Malformed, false, func() error {
			_, err := DecodeRequest(nil, 0)
			return err
		}},
		{"version", "unsupported_version", UnsupportedVersion, false, func() error {
			_, err := DecodeRequest([]byte(`{"v":2,"action":"subscribe","id":"request","channel":"chat"}`), 1024)
			return err
		}},
		{"readiness", "stopping", Stopping, false, func() error {
			hub := &Hub{done: make(chan struct{}), closing: true}
			return hub.Probe(t.Context())
		}},
		{"publication", "invalid: WebSocket publication requires a publisher", fault.Invalid, true, func() error {
			_, err := Publish(t.Context(), nil, Channel[struct{}, string, Anonymous]{}, "room", Outgoing[struct{}, string]{}, "payload")
			return err
		}},
		{"distributed exclusion", "invalid: excluding a connection of another instance requires ClusterConfig.ExcludeRemoteConnections", fault.Invalid, true, func() error {
			hub := &Hub{registry: &Registry{channels: map[ChannelID]*channelDefinition{"chat": {}}}, cluster: &clusterState{}}
			return hub.publishDistributed(t.Context(), Response{Channel: "chat"}, nil, connection)
		}},
	} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/goexit=%t", test.name, exit), func(t *testing.T) {
				original := test.run()
				private := &privateSocketDiagnostic{exit: exit}
				var detail *fault.Error
				if errors.As(original, &detail) {
					*detail = *fault.Wrap(fault.Invalid, "private replacement", private)
				}
				result := callback.Isolated("consumer", func() error { return original })
				want := "consumer: " + test.message
				if result.Error() != want || !errors.Is(result, original) || !errors.Is(result, test.classification) || NotPublished(result) != test.notPublished {
					t.Fatal("socket pipeline lost diagnostic, classification or publication identity")
				}
				if detail != nil && (!errors.Is(result, detail) || !errors.Is(result, private)) {
					t.Fatal("socket pipeline lost typed diagnostic or cause")
				}
				if fmt.Sprintf("%v %+v %#v", result, result, result) != want+" "+want+" "+want || private.calls.Load() != 0 {
					t.Fatal("socket pipeline formatted application text")
				}
				app := &notPublished{cause: private}
				redacted := callback.Isolated("consumer", func() error { return app })
				if redacted.Error() != "consumer failed" || !errors.Is(redacted, private) || !NotPublished(redacted) || private.calls.Load() != 0 {
					t.Fatal("application publication cause escaped containment")
				}
			})
		}
	}
}
