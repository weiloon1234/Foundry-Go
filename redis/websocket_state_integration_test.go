package redis

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func websocketAuthority(t *testing.T) (*Client, *WebSocketBackend, ws.ClusterKey, func(string)) {
	t.Helper()
	client, namespace, track := integrationAddresses(t, nil)
	backend, err := NewWebSocketBackend(client)
	if err != nil {
		t.Fatal(err)
	}
	limits := ws.ClusterLimits{Connections: 3, ConnectionsPerSubject: 2, Subscriptions: 4, PresenceMembers: 4, MemberBytes: 128, FrameBytes: 8192, ConnectionTTL: 200 * time.Millisecond, Retention: 2 * time.Second}
	key, err := ws.NewClusterKey(namespace, strings.Repeat("a", 64), limits)
	if err != nil {
		t.Fatal(err)
	}
	track(key.String() + ":metadata")
	track(key.String() + ":connections")
	return client, backend, key, track
}
func websocketID[T any](t *testing.T) model.ID[T] {
	t.Helper()
	id, err := model.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func websocketOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRedisWebSocketLeaseExpiryDoesNotResurrectOrDeleteForeignOwner(t *testing.T) {
	_, backend, key, track := websocketAuthority(t)
	ctx := t.Context()
	instance, foreign := websocketID[ws.Instance](t), websocketID[ws.Instance](t)
	one, two, three := websocketID[ws.Connection](t), websocketID[ws.Connection](t), websocketID[ws.Connection](t)
	subject := ws.MemberID(strings.Repeat("b", 64))
	scope := ws.Scope{Channel: "members", Room: "1", HasRoom: true}
	data := json.RawMessage(`{"integer":9007199254740993,"decimal":"123.4500"}`)
	membership := ws.ClusterMembership{Scope: scope, Subject: subject, Presence: true, Data: data}
	for _, id := range []ws.ConnectionID{one, two, three} {
		track(key.String() + ":connection:" + id.String())
		websocketOK(t, backend.WebSocketOpen(ctx, key, instance, id))
	}
	track(key.String() + ":subject:" + string(subject))
	track(key.String() + ":presence:" + scope.Hash() + ":leases")
	track(key.String() + ":presence:" + scope.Hash() + ":data")
	initial, err := backend.WebSocketJoin(ctx, key, instance, one, membership)
	websocketOK(t, err)
	second, err := backend.WebSocketJoin(ctx, key, instance, two, membership)
	websocketOK(t, err)
	if len(second.Members) != 1 || second.Members[0].Connections != 2 || !bytes.Equal(second.Members[0].Data, data) || second.Revision <= initial.Revision {
		t.Fatal("authority rounded DTO or miscounted tabs")
	}
	if _, err := backend.WebSocketJoin(ctx, key, instance, three, membership); !errors.Is(err, ws.CapacityExceeded) {
		t.Fatal("subject quota bypass", err)
	}
	if err := backend.WebSocketClose(ctx, key, foreign, one); !errors.Is(err, ws.Stopping) {
		t.Fatal("foreign owner closed connection", err)
	}
	websocketOK(t, backend.WebSocketLeave(ctx, key, instance, one, scope))
	remaining, err := backend.WebSocketMembers(ctx, key, scope)
	websocketOK(t, err)
	if len(remaining.Members) != 1 || remaining.Members[0].Connections != 1 {
		t.Fatal("first tab removed subject")
	}
	// Simulated process loss: no Close or Touch for the remaining lease.
	time.Sleep(250 * time.Millisecond)
	expired, err := backend.WebSocketMembers(ctx, key, scope)
	websocketOK(t, err)
	if len(expired.Members) != 0 || expired.Revision <= remaining.Revision {
		t.Fatal("stale process presence survived lease")
	}
	if err := backend.WebSocketTouch(ctx, key, instance, two); !errors.Is(err, ws.Stopping) {
		t.Fatal("expired connection resurrected", err)
	}
	websocketOK(t, backend.WebSocketClose(ctx, key, instance, two))
	websocketOK(t, backend.WebSocketClose(ctx, key, instance, two))
}
func TestRedisWebSocketReplayCountBytesTTLAndIdempotentAppend(t *testing.T) {
	_, backend, key, track := websocketAuthority(t)
	ctx := t.Context()
	channel := ws.ChannelID("history")
	for _, suffix := range []string{"order", "expiry", "data", "metadata"} {
		track(key.String() + ":history:" + string(channel) + ":" + suffix)
	}
	policy := ws.ReplayConfig{Messages: 2, Bytes: 1024, TTL: 100 * time.Millisecond}
	frame := func(text string) []byte {
		id := websocketID[ws.Publication](t)
		data, err := json.Marshal(ws.Response{Version: 1, Type: ws.EventResponse, Channel: channel, Event: "updated", MessageID: id, Payload: json.RawMessage(`{"text":"` + text + `","integer":9007199254740993}`)})
		websocketOK(t, err)
		return data
	}
	one, two, three := frame("one"), frame("two"), frame("three")
	for _, data := range [][]byte{one, two, two, three} {
		websocketOK(t, backend.WebSocketAppend(ctx, key, channel, policy, data))
	}
	history, err := backend.WebSocketHistory(ctx, key, channel, policy)
	websocketOK(t, err)
	if len(history) != 2 || !bytes.Equal(history[0], two) || !bytes.Equal(history[1], three) {
		t.Fatal("history count/order/precision changed")
	}
	changed := bytes.Replace(three, []byte("three"), []byte("other"), 1)
	if err := backend.WebSocketAppend(ctx, key, channel, policy, changed); !errors.Is(err, fault.Invalid) {
		t.Fatal("same identity changed payload", err)
	}
	time.Sleep(130 * time.Millisecond)
	history, err = backend.WebSocketHistory(ctx, key, channel, policy)
	websocketOK(t, err)
	if len(history) != 0 {
		t.Fatal("history outlived TTL")
	}
	// Byte budget evicts before message-count budget.
	policy.Bytes = len(one) + len(two) - 1
	for _, data := range [][]byte{one, two} {
		websocketOK(t, backend.WebSocketAppend(ctx, key, channel, policy, data))
	}
	history, err = backend.WebSocketHistory(ctx, key, channel, policy)
	websocketOK(t, err)
	if len(history) != 1 || !bytes.Equal(history[0], two) {
		t.Fatal("history byte budget bypass")
	}
}
func TestRedisWebSocketRejectsCorruptMetadataAndPolicyWithoutResettingIt(t *testing.T) {
	for _, test := range []struct {
		name, field, value string
		want               error
	}{
		{"policy", "policy", strings.Repeat("c", 64), fault.Conflict},
		{"revision", "revision", "9007199254740992", fault.Invalid},
		{"clock", "time", "253402300799999", fault.Conflict},
		{"oversized", "limits", strings.Repeat("x", 2049), fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, backend, key, _ := websocketAuthority(t)
			websocketOK(t, backend.WebSocketCheck(t.Context(), key))
			address := key.String() + ":metadata"
			websocketOK(t, client.raw.HSet(t.Context(), address, test.field, test.value).Err())
			err := backend.WebSocketCheck(t.Context(), key)
			if !errors.Is(err, test.want) {
				t.Fatal("corruption not rejected", err)
			}
			if got := client.raw.HGet(t.Context(), address, test.field).Val(); got != test.value {
				t.Fatal("corrupt state silently reset")
			}
		})
	}
}
