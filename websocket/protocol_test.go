package websocket_test

import (
	"errors"
	"strings"
	"testing"

	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestProtocolUsesExactBoundedVersionedEnvelope(t *testing.T) {
	for _, test := range []struct {
		name, data string
		code       ws.Code
	}{
		{"subscribe", `{"v":1,"action":"subscribe","id":"a","channel":"chat","room":"room-1"}`, ""},
		{"message", `{"v":1,"action":"message","id":"a","channel":"chat","event":"send","payload":{"text":"hello"}}`, ""},
		{"replay", `{"v":1,"action":"subscribe","id":"a","channel":"chat","replay":2}`, ""},
		{"null-replay", `{"v":1,"action":"subscribe","id":"a","channel":"chat","replay":null}`, ws.Malformed},
		{"negative-replay", `{"v":1,"action":"subscribe","id":"a","channel":"chat","replay":-1}`, ws.Malformed},
		{"oversized-replay", `{"v":1,"action":"subscribe","id":"a","channel":"chat","replay":1025}`, ws.Malformed},
		{"unsubscribe-replay", `{"v":1,"action":"unsubscribe","id":"a","channel":"chat","replay":0}`, ws.Malformed},

		{"version", `{"v":2,"action":"subscribe","id":"a","channel":"chat"}`, ws.UnsupportedVersion},
		{"fractional", `{"v":1.0,"action":"subscribe","id":"a","channel":"chat"}`, ws.Malformed},
		{"duplicate", `{"v":1,"v":1,"action":"subscribe","id":"a","channel":"chat"}`, ws.Malformed},
		{"case", `{"v":1,"Action":"subscribe","id":"a","channel":"chat"}`, ws.Malformed},
		{"extra", `{"v":1,"action":"subscribe","id":"a","channel":"chat","credential":"secret"}`, ws.Malformed},
		{"null-room", `{"v":1,"action":"subscribe","id":"a","channel":"chat","room":null}`, ws.Malformed},
		{"empty-room", `{"v":1,"action":"subscribe","id":"a","channel":"chat","room":""}`, ws.Malformed},
		{"control", `{"v":1,"action":"subscribe","id":"a","channel":"chat","room":"a\u0000b"}`, ws.Malformed},
		{"no-payload", `{"v":1,"action":"message","id":"a","channel":"chat","event":"send"}`, ws.Malformed},
		{"wrong-fields", `{"v":1,"action":"unsubscribe","id":"a","channel":"chat","payload":null}`, ws.Malformed},
		{"unicode", `{"v":1,"action":"message","id":"a","channel":"chat","event":"send","payload":"\ud800"}`, ws.Malformed},
		{"bad-id", `{"v":1,"action":"subscribe","id":"secret token","channel":"chat"}`, ws.Malformed},
		{"array", `[]`, ws.Malformed},
		{"trailing", `{} {}`, ws.Malformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ws.DecodeRequest([]byte(test.data), 4096)
			if test.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, test.code) {
				t.Fatalf("expected %s, got %v", test.code, err)
			}
		})
	}
	if _, err := ws.DecodeRequest([]byte(strings.Repeat(" ", 4097)), 4096); !errors.Is(err, ws.Malformed) {
		t.Fatal("frame byte bound ignored")
	}
}
func FuzzProtocolFrame(f *testing.F) {
	f.Add([]byte(`{"v":1,"action":"subscribe","id":"a","channel":"chat","replay":2}`))
	f.Add([]byte(`{"v":1,"action":"subscribe","id":"1","channel":"chat"}`))
	f.Add([]byte(`{"v":1,"action":"message","id":"2","channel":"chat","event":"send","payload":{}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		request, err := ws.DecodeRequest(data, 64<<10)
		if err == nil {
			if request.Version != ws.ProtocolVersion || request.ID == "" || request.Channel == "" {
				t.Fatal("incomplete accepted frame")
			}
			if request.Action != ws.Subscribe && request.Action != ws.Unsubscribe && request.Action != ws.Message {
				t.Fatal("unknown accepted action")
			}
		}
	})
}
