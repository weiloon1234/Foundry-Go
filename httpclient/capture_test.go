package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

// captureSuiteEnv makes newClient build every client through Module with a
// capture recorder that keeps every body byte, so the whole suite proves
// capture changes nothing a caller, server or transport observes.
const captureSuiteEnv = "FOUNDRY_HTTPCLIENT_CAPTURE_SUITE"

func capturingSuite() bool { return os.Getenv(captureSuiteEnv) == "1" }

func TestExistingSuitesUnchangedUnderCapture(t *testing.T) {
	if capturingSuite() {
		t.Skip("this process is the captured run")
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^Test", "-test.count=1")
	command.Env = append(os.Environ(), captureSuiteEnv+"=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("httpclient suites changed behavior under capture: %v\n%s", err, output)
	}
}

type captureSink struct {
	recorder *httpcapture.Outbound
	mu       sync.Mutex
	records  []httpcapture.Exchange
	done     chan struct{}
}

// newCaptureSink drains the recorder like the store's writer does, keeping
// the snapshots for assertions.
func newCaptureSink(t testing.TB, full ...string) *captureSink {
	t.Helper()
	limits := httpcapture.Config{BodyBytes: 1 << 20, RetainedBytes: 64 << 20, QueueSize: 256, Rate: 10000, Burst: 10000, LaneShare: httpcapture.OutboundLaneShare}
	recorder, err := httpcapture.NewOutbound(httpcapture.OutboundConfig{Limits: limits, Full: full, Inspect: func(string, string) httpcapture.Skip { return httpcapture.Kept }})
	if err != nil {
		t.Fatal(err)
	}
	sink := &captureSink{recorder: recorder, done: make(chan struct{})}
	go func() {
		defer close(sink.done)
		queue := recorder.Recorder()
		for {
			select {
			case <-queue.Ready():
				sink.drain()
			case <-queue.Done():
				sink.drain()
				return
			}
		}
	}()
	t.Cleanup(func() { recorder.Recorder().Seal(); <-sink.done })
	return sink
}

func (s *captureSink) drain() {
	for record := s.recorder.Recorder().Drain(); record != nil; record = s.recorder.Recorder().Drain() {
		s.mu.Lock()
		s.records = append(s.records, record.Snapshot)
		s.mu.Unlock()
		record.Release()
	}
}

func (s *captureSink) Exchanges() []httpcapture.Exchange {
	s.drain()
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]httpcapture.Exchange(nil), s.records...)
}

// capturedClient builds config's client through Module inside an assembly
// that contributes sink's recorder, as application assembly does.
func capturedClient(t testing.TB, sink *captureSink, config httpclient.Config, transport http.RoundTripper) *httpclient.Client {
	t.Helper()
	contribution := foundation.Module{Name: "test.capture-recorder", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Contribute(r, httpcapture.OutboundSinks, "recorder", func(foundation.Resolver) (*httpcapture.Outbound, error) { return sink.recorder, nil })
	}}
	var adapter func(foundation.Resolver) (http.RoundTripper, error)
	if transport != nil {
		adapter = func(foundation.Resolver) (http.RoundTripper, error) { return transport, nil }
	}
	key := foundation.NewKey[*httpclient.Client]("test.captured-client")
	app, err := foundation.NewBuilder().Register(contribution, httpclient.Module("test.captured-client", key, config, nil, adapter)).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOnlyModuleClientsInsideAnAssemblyAreCaptured(t *testing.T) {
	var flaky atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/flaky" && flaky.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(server.Close)
	config := testConfig()
	config.BaseURL = server.URL + "/v1"
	sink := newCaptureSink(t, string(config.Name))
	captured := capturedClient(t, sink, config, nil)
	t.Cleanup(func() { _ = captured.Close(context.Background()) })
	plain := newClient(t, config, nil)
	if !capturingSuite() {
		if _, err := plain.Do(t.Context(), plain.Get("ok")); err != nil {
			t.Fatal(err)
		}
		if len(sink.Exchanges()) != 0 {
			t.Fatal("a client built by New was captured")
		}
	}
	response, err := captured.Do(t.Context(), captured.Get("flaky"))
	if err != nil || response.Status() != 200 || response.Attempts() != 2 {
		t.Fatal("retried call changed", err, response.Attempts())
	}
	exchanges := sink.Exchanges()
	if len(exchanges) != 2 || exchanges[0].Operation != exchanges[1].Operation || exchanges[0].ID == exchanges[1].ID {
		t.Fatal("attempts were not one row each in one operation", len(exchanges))
	}
	first, second := exchanges[0], exchanges[1]
	firstStatus, _ := first.Status.Get()
	secondStatus, _ := second.Status.Get()
	if first.Attempt != 1 || !first.Retried || firstStatus != 503 || second.Attempt != 2 || second.Retried || secondStatus != 200 {
		t.Fatal("attempt numbers, retry flags or statuses changed", first.Attempt, first.Retried, second.Attempt)
	}
}
