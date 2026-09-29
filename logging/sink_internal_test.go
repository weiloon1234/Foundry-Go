package logging

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type failingDestination struct{ calls int }

func (d *failingDestination) writeRecord([]byte, slog.Level) (int, error) {
	d.calls++
	return 0, &os.PathError{Op: "write", Path: "/private/secret-path", Err: syscall.ENOSPC}
}

// startedSink prepares a stream sink, then substitutes an owned test destination
// and capture writers before any record is admitted.
func startedSink(t *testing.T, config SinkConfig, target destination) (*Sink, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	sink, err := PrepareSink(config)
	if err != nil {
		t.Fatal(err)
	}
	var fallback, notices bytes.Buffer
	state := sink.state
	state.mu.Lock()
	state.target, state.fallback, state.events.notices = target, &fallback, &notices
	state.startAsync()
	state.mu.Unlock()
	t.Cleanup(func() { _ = sink.Close() })
	return sink, &fallback, &notices
}

func TestFailedWritesAreCountedAndFallBackOnce(t *testing.T) {
	target := &failingDestination{}
	sink, fallback, notices := startedSink(t, SinkConfig{Driver: Stdout}, target)
	logger := sink.Logger()
	logger.Info("first-record")
	logger.Warn("second-record")
	if _, err := sink.Write(bytes.Repeat([]byte("x"), MaxFallbackBytes+1)); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("destination failure was hidden", err)
	}
	stats := sink.Stats()
	if stats.Driver != Stdout || stats.Records != 0 || stats.Failures != 3 || stats.Fallbacks != 2 || stats.Dropped != 1 {
		t.Fatal("failure accounting is wrong", stats)
	}
	if !strings.Contains(fallback.String(), "first-record") || !strings.Contains(fallback.String(), "second-record") || strings.Contains(fallback.String(), "xxxx") {
		t.Fatal("bounded stderr fallback lost records", fallback.String())
	}
	if strings.Count(notices.String(), "log destination write failed") != 1 || !strings.Contains(notices.String(), "no space left on device") || strings.Contains(notices.String(), "secret-path") {
		t.Fatal("notice was not a single safe diagnostic", notices.String())
	}
}

func TestStderrSinkHasNoSelfFallback(t *testing.T) {
	sink, err := PrepareSink(SinkConfig{Driver: Stderr})
	if err != nil {
		t.Fatal(err)
	}
	if sink.state.fallback != nil {
		t.Fatal("stderr sink would duplicate failed records onto itself")
	}
}

// blockingDestination holds the async writer inside its first record.
type blockingDestination struct {
	mu      sync.Mutex
	entered chan struct{}
	release chan struct{}
	records []string
}

func (d *blockingDestination) writeRecord(data []byte, _ slog.Level) (int, error) {
	d.mu.Lock()
	first := len(d.records) == 0
	d.records = append(d.records, string(data))
	d.mu.Unlock()
	if first {
		close(d.entered)
		<-d.release
	}
	return len(data), nil
}

func TestAsyncSinkDropsNewestWhenFullAndDrainsOnClose(t *testing.T) {
	target := &blockingDestination{entered: make(chan struct{}), release: make(chan struct{})}
	sink, _, notices := startedSink(t, SinkConfig{Driver: Stdout, Async: AsyncConfig{Enabled: true, Queue: 1}}, target)
	record := func(text string) error { _, err := sink.Write([]byte(text)); return err }
	if err := record("one"); err != nil {
		t.Fatal(err)
	}
	<-target.entered
	if err := record("two"); err != nil {
		t.Fatal(err)
	}
	if err := record("three"); !errors.Is(err, fault.Overloaded) {
		t.Fatal("full queue blocked or accepted a record", err)
	}
	if stats := sink.Stats(); stats.Dropped != 1 || stats.Queued != 1 {
		t.Fatal("drop or backlog was not reported", stats)
	}
	close(target.release)
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(target.records, ",") != "one,two" {
		t.Fatal("close did not drain accepted records in order", target.records)
	}
	if stats := sink.Stats(); stats.Records != 2 || stats.Dropped != 1 || stats.Queued != 0 {
		t.Fatal("async accounting is wrong", stats)
	}
	if strings.Count(notices.String(), "async log queue is full") != 1 {
		t.Fatal("drop notice missing", notices.String())
	}
	if err := record("after"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("closed async sink accepted a record", err)
	}
}

func TestAsyncConfigValidation(t *testing.T) {
	for _, config := range []AsyncConfig{{Queue: 4}, {Enabled: true, Queue: -1}, {Enabled: true, Queue: 65537}, {Enabled: true, MaxBytes: 10}} {
		if err := config.Validate(); err == nil {
			t.Fatal("invalid async configuration accepted", config)
		}
	}
	if err := (AsyncConfig{Enabled: true}).Validate(); err != nil {
		t.Fatal(err)
	}
	stack := ChannelSettings{Sink: SinkConfig{Driver: Stack, Async: AsyncConfig{Enabled: true}}, Stack: []ChannelName{"a"}}
	if err := stack.Validate(); err == nil {
		t.Fatal("stack accepted leaf-owned async options")
	}
	if err := (SinkConfig{Driver: Stdout, Syslog: SyslogConfig{Tag: "app"}}).Validate(); err == nil {
		t.Fatal("stream accepted syslog options")
	}
}
