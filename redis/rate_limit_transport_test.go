package redis

import (
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRateLimitLostReplyIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "eval") {
			calls.Add(1)
			conn.Close()
			return
		}
		io.WriteString(conn, "+PONG\r\n")
	})
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	k, _ := ratelimit.NewKey(keyspace.Namespace{Application: "test", Environment: "lost-reply"}, "requests", "key")
	if d, err := c.RateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); err == nil || d != (ratelimit.Decision{}) || calls.Load() != 1 {
		t.Fatal(d, err, calls.Load())
	}
}
func TestRateLimitMalformedTransportReplies(t *testing.T) {
	for _, reply := range []string{"+invalid\r\n", "*0\r\n", "*1\r\n:9\r\n", "*3\r\n:1\r\n:-1\r\n:1000\r\n", "*3\r\n:0\r\n:1\r\n:1000\r\n", "*3\r\n:1\r\n:0\r\n:1001\r\n"} {
		t.Run(reply, func(t *testing.T) {
			config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
				if strings.EqualFold(args[0], "eval") {
					io.WriteString(conn, reply)
					return
				}
				io.WriteString(conn, "+PONG\r\n")
			})
			c := preparedTransport(t, config)
			if err := c.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			k, _ := ratelimit.NewKey(keyspace.Namespace{Application: "test", Environment: "invalid-reply"}, "requests", "key")
			if d, err := c.RateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); err == nil || d != (ratelimit.Decision{}) {
				t.Fatal(d, err)
			}
		})
	}
}
