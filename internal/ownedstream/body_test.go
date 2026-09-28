package ownedstream_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ownedstream"
)

type body struct {
	read  func([]byte) (int, error)
	close func() error
}

func (b body) Read(p []byte) (int, error) { return b.read(p) }
func (b body) Close() error               { return b.close() }

func receive(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not reach the expected phase")
	}
}

func TestCancellationClosesOnceAndWaitsForActualReadReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, closed, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var closes atomic.Int32
	raw := body{read: func([]byte) (int, error) {
		close(entered)
		<-closed
		<-release
		return 0, io.EOF
	}, close: func() error {
		closes.Add(1)
		close(closed)
		return nil
	}}
	stream, err := ownedstream.New(ctx, raw, 4, -1)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := stream.Read(make([]byte, 4))
		if !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
		close(finished)
	}()
	receive(t, entered)
	cancel()
	receive(t, closed)
	drained := make(chan struct{})
	go func() { _ = stream.Close(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("close abandoned the unfinished read")
	default:
	}
	once.Do(func() { close(release) })
	receive(t, finished)
	receive(t, drained)
	if err := stream.Close(); err != nil || closes.Load() != 1 {
		t.Fatal(err, closes.Load())
	}
	if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}

func TestReadBoundsAndFailuresRemainSticky(t *testing.T) {
	for _, test := range []struct {
		name, text        string
		maximum, expected int64
		valid             bool
	}{
		{"unknown", "body", 4, -1, true}, {"exact", "body", 4, 4, true},
		{"empty", "", 4, 0, true}, {"over-limit", "body!", 4, -1, false},
		{"long", "body", 4, 3, false}, {"short", "body", 8, 5, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream, err := ownedstream.New(t.Context(), io.NopCloser(strings.NewReader(test.text)), test.maximum, test.expected)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			data, err := io.ReadAll(stream)
			if test.valid {
				if err != nil || string(data) != test.text || stream.Err() != nil {
					t.Fatal(string(data), err)
				}
			} else {
				if err == nil || stream.Err() == nil {
					t.Fatal("failure disappeared")
				}
				if _, err := stream.Read(make([]byte, 4)); err == nil {
					t.Fatal("failed read recovered")
				}
			}
		})
	}
}

func TestForeignReadAndCloseFailuresAreContained(t *testing.T) {
	for _, mode := range []string{"panic", "goexit", "negative-count", "excess-count"} {
		t.Run(mode, func(t *testing.T) {
			var closes atomic.Int32
			stream, err := ownedstream.New(t.Context(), body{read: func(p []byte) (int, error) {
				switch mode {
				case "panic":
					panic("private read payload")
				case "goexit":
					runtime.Goexit()
				case "negative-count":
					return -1, nil
				}
				return len(p) + 1, nil
			}, close: func() error { closes.Add(1); return nil }}, 4, -1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Read(make([]byte, 4)); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if err := stream.Close(); err != nil || closes.Load() != 1 {
				t.Fatal(err, closes.Load())
			}
		})
	}
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run("close-"+mode, func(t *testing.T) {
			stream, err := ownedstream.New(t.Context(), body{read: func([]byte) (int, error) { return 0, io.EOF }, close: func() error {
				switch mode {
				case "panic":
					panic("private close payload")
				case "goexit":
					runtime.Goexit()
				}
				return errors.New("private close error")
			}}, 4, -1)
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Close(); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if err := stream.Close(); err == nil {
				t.Fatal("close failure disappeared")
			}
		})
	}
}
