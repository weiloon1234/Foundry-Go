package websocket_test

import (
	"reflect"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestClientMetadataUsesActualConfigAndOwnedProtocol(t *testing.T) {
	channel := ws.Public[struct{}]("metadata", ws.DefineRooms(foundryhttp.StringPath[string]()))
	registry, err := ws.NewRegistry(ws.Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	config := ws.DefaultConfig()
	config.MaxSubscriptions = 17
	config.OperationTimeout = 333 * time.Millisecond
	info, err := ws.DescribeClient(registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if info.Protocol.Version != ws.ProtocolVersion || info.Protocol.Actions.Message != ws.Message || info.Protocol.Responses.Acknowledged != ws.Acknowledged || info.Protocol.Subprotocol != ws.Subprotocol {
		t.Fatal("protocol vocabulary diverged")
	}
	if info.Limits.Subscriptions != 17 || info.Limits.OperationMilliseconds != 333 || !reflect.DeepEqual(info.Limits.Payload, config.Payload) {
		t.Fatal("configured limits replaced with defaults")
	}
	info.Protocol.Codes[0] = "changed"
	if ws.ProtocolDescription().Codes[0] == "changed" {
		t.Fatal("protocol metadata aliases shared storage")
	}
	config.MaxFrameBytes = 0
	if _, err := ws.DescribeClient(registry, config); err == nil {
		t.Fatal("invalid runtime config exported")
	}
	if _, err := ws.DescribeClient(nil, ws.DefaultConfig()); err == nil {
		t.Fatal("nil registry accepted")
	}
}
