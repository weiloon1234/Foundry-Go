package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// transportServer is an owned protocol fault fixture, not another Redis service.
func transportServer(t *testing.T, command func(net.Conn, []string, <-chan struct{})) Config {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	done := make(chan struct{})
	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			wg.Go(func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					args, err := readCommand(reader)
					if err != nil {
						return
					}
					if strings.EqualFold(args[0], "hello") {
						io.WriteString(conn, "*0\r\n")
						continue
					}
					if strings.EqualFold(args[0], "evalsha") {
						// Scripts start with EVALSHA; an uncached script falls back to EVAL.
						io.WriteString(conn, "-NOSCRIPT No matching script. Please use EVAL.\r\n")
						continue
					}
					command(conn, args, done)
				}
			})
		}
	})
	t.Cleanup(func() {
		close(done)
		listener.Close()
		mu.Lock()
		for _, conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	address := listener.Addr().(*net.TCPAddr)
	c := explicitConfig()
	c.Port = uint16(address.Port)
	return c
}
func readCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 4 || line[0] != '*' {
		return nil, fmt.Errorf("invalid command framing")
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || count < 1 || count > 128 {
		return nil, fmt.Errorf("invalid command length")
	}
	values := make([]string, count)
	for i := range values {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(line) < 4 || line[0] != '$' {
			return nil, fmt.Errorf("invalid bulk framing")
		}
		size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || size < 0 || size > 1<<20 {
			return nil, fmt.Errorf("invalid bulk length")
		}
		value := make([]byte, size+2)
		if _, err = io.ReadFull(reader, value); err != nil {
			return nil, err
		}
		values[i] = string(value[:size])
	}
	return values, nil
}
func preparedTransport(t *testing.T, c Config) *Client {
	t.Helper()
	client, err := Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return client
}
func TestConcurrentStartupCloseRetainsAttemptOwnership(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c := transportServer(t, func(conn net.Conn, args []string, done <-chan struct{}) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			io.WriteString(conn, "+PONG\r\n")
		case <-done:
		}
	})
	client := preparedTransport(t, c)
	started := make(chan error, 1)
	go func() { started <- client.Start(t.Context()) }()
	<-entered
	waiting, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.Start(waiting); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := client.Close(waiting); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := client.Ping(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	select {
	case <-client.Done():
		t.Fatal("close abandoned active startup")
	default:
	}
	close(release)
	if err := <-started; !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := client.Stats(); s.Operations != 0 || s.Ready || !s.Closing {
		t.Fatal(s)
	}
}
func TestOperationCapacityAndCloseDrain(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var pings atomic.Int64
	c := transportServer(t, func(conn net.Conn, args []string, done <-chan struct{}) {
		if pings.Add(1) > 1 {
			close(entered)
			select {
			case <-release:
			case <-done:
				return
			}
		}
		io.WriteString(conn, "+PONG\r\n")
	})
	c.MaxConnections = 1
	c.MaxOperations = 1
	client := preparedTransport(t, c)
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	operation := make(chan error, 1)
	go func() { operation <- client.Ping(t.Context()) }()
	<-entered
	// A full bound queues for a bounded time, then reports retryable overload.
	bounded, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	err := client.Ping(bounded)
	stop()
	if !errors.Is(err, fault.Overloaded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("operation bound ignored", err)
	}
	waiting := make(chan error, 1)
	go func() { waiting <- client.Ping(t.Context()) }()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Shutdown stops queued admission instead of starting new commands.
	if err := <-waiting; !errors.Is(err, fault.Closed) {
		t.Fatal("queued operation survived shutdown", err)
	}
	select {
	case <-client.Done():
		t.Fatal("close abandoned command")
	default:
	}
	close(release)
	if err := <-operation; err != nil {
		t.Fatal(err)
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-client.Done()
}
func TestLostMutationReplyNeverRetries(t *testing.T) {
	var mutations atomic.Int64
	c := transportServer(t, func(conn net.Conn, args []string, done <-chan struct{}) {
		if strings.EqualFold(args[0], "eval") {
			mutations.Add(1)
			conn.Close()
			return
		}
		io.WriteString(conn, "+PONG\r\n")
	})
	client := preparedTransport(t, c)
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	key, err := cache.NewEntryKey(cache.Namespace{Application: "foundry", Environment: "transport"}, "faults", "write")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Put(t.Context(), key, []byte("owned"), cache.Forever()); err == nil {
		t.Fatal("lost reply claimed success")
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if mutations.Load() != 1 {
		t.Fatal("uncertain mutation retried", mutations.Load())
	}
}
func TestStalledResponseHonorsDeadline(t *testing.T) {
	entered := make(chan struct{})
	var pings atomic.Int64
	c := transportServer(t, func(conn net.Conn, args []string, done <-chan struct{}) {
		if pings.Add(1) == 1 {
			io.WriteString(conn, "+PONG\r\n")
			return
		}
		close(entered)
		<-done
	})
	client := preparedTransport(t, c)
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.Ping(ctx) }()
	select {
	case <-entered:
	case err := <-result:
		t.Fatal("command failed before the stalled response", err)
	}
	if err := <-result; !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, fault.Timeout) {
		t.Fatal(err)
	}
	if client.Stats().Operations != 0 {
		t.Fatal("timeout leaked owner")
	}
}
func TestServerErrorsDoNotExposePayloads(t *testing.T) {
	c := transportServer(t, func(conn net.Conn, args []string, done <-chan struct{}) {
		io.WriteString(conn, "-ERR secret-fixture-value\r\n")
	})
	client := preparedTransport(t, c)
	err := client.Start(t.Context())
	if !errors.Is(err, fault.Internal) || strings.Contains(fmt.Sprintf("%+v %#v", err, err), "secret-fixture-value") {
		t.Fatal("unsafe error", err)
	}
	if again := client.Start(t.Context()); again != err {
		t.Fatal("terminal startup retried")
	}
}
