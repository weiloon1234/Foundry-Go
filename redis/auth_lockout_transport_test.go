package redis

import (
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/internal/lockouttest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

func TestLockoutMalformedTransportReplies(t *testing.T) {
	for _, reply := range []string{"+invalid\r\n", "*0\r\n", "*1\r\n:9\r\n", "*1\r\n:1\r\n", "*2\r\n:2\r\n:-1\r\n", "*3\r\n:1\r\n+invalid-generation\r\n:0\r\n"} {
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
			key, err := lockout.NewKey(keyspace.Namespace{Application: "test", Environment: "reply"}, "password", "member")
			if err != nil {
				t.Fatal(err)
			}
			if a, err := c.LockoutBegin(t.Context(), key, lockout.DefaultPolicy(), lockouttest.Generation(t)); err == nil || a != (lockout.Admission{}) {
				t.Fatal(a, err)
			}
		})
	}
}
func TestLockoutLostAdmissionReplyIsNotRetried(t *testing.T) {
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
	key, err := lockout.NewKey(keyspace.Namespace{Application: "test", Environment: "lost"}, "password", "member")
	if err != nil {
		t.Fatal(err)
	}
	if a, err := c.LockoutBegin(t.Context(), key, lockout.DefaultPolicy(), lockouttest.Generation(t)); err == nil || a != (lockout.Admission{}) || calls.Load() != 1 {
		t.Fatal(a, err, calls.Load())
	}
}
