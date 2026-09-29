package redis

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

// The script returns members in table iteration order; the adapter orders
// them by bytes (uppercase before lowercase) independent of Redis locale.
func TestWebSocketPresenceOrdersMembersByBytes(t *testing.T) {
	limits := ws.ClusterLimits{Connections: 8, ConnectionsPerSubject: 2, Subscriptions: 4, PresenceMembers: 4, MemberBytes: 64, FrameBytes: 8192, ConnectionTTL: time.Second, Retention: 2 * time.Second}
	key, err := ws.NewClusterKey(keyspace.Namespace{Application: "order", Environment: "test"}, strings.Repeat("a", 64), limits)
	if err != nil {
		t.Fatal(err)
	}
	var reply websocketReply
	for _, id := range []string{"b", "B", "a", "A", "0"} {
		reply.Members = append(reply.Members, struct {
			ID          ws.MemberID `json:"id"`
			Data        string      `json:"data"`
			Connections int         `json:"connections"`
		}{ws.MemberID(id), `{}`, 1})
	}
	reply.Revision = 3
	snapshot, err := websocketPresence(reply, key)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, member := range snapshot.Members {
		ids = append(ids, string(member.ID))
	}
	if !slices.Equal(ids, []string{"0", "A", "B", "a", "b"}) {
		t.Fatal("members were not ordered by bytes", ids)
	}
	reply.Members = append(reply.Members, reply.Members[0])
	if _, err := websocketPresence(reply, key); err == nil {
		t.Fatal("a repeated member was accepted")
	}
}
