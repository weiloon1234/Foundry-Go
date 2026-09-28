package redis

import (
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func TestLeaseLostResponsesAreNotRetried(t *testing.T) {
	for _, op := range []string{"acquire", "renew", "release"} {
		t.Run(op, func(t *testing.T) {
			var mutations atomic.Int32
			config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
				if strings.EqualFold(args[0], "eval") {
					mutations.Add(1)
					conn.Close()
					return
				}
				io.WriteString(conn, "+PONG\r\n")
			})
			c := preparedTransport(t, config)
			if err := c.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			key, _ := lease.NewKey(keyspace.Namespace{Application: "test", Environment: "lost-reply"}, "leases", "a")
			owner, _ := lease.NewOwner()
			var ok bool
			var err error
			switch op {
			case "acquire":
				ok, err = c.LeaseAcquire(t.Context(), key, owner, time.Second)
			case "renew":
				ok, err = c.LeaseRenew(t.Context(), key, owner, time.Second)
			case "release":
				ok, err = c.LeaseRelease(t.Context(), key, owner)
			}
			if ok || err == nil || mutations.Load() != 1 {
				t.Fatal(ok, err, mutations.Load())
			}
		})
	}
}
