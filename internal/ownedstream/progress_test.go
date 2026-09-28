package ownedstream_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/ownedstream"
)

func TestNonProgressingBodyFailsAndRetainsFailure(t *testing.T) {
	calls, closes := 0, 0
	stream, err := ownedstream.New(t.Context(), body{read: func([]byte) (int, error) { calls++; return 0, nil }, close: func() error { closes++; return nil }}, 10, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	buffer := make([]byte, 4)
	for range 100 {
		_, err = stream.Read(buffer)
		if err != nil {
			break
		}
	}
	if !errors.Is(err, io.ErrNoProgress) || !errors.Is(stream.Err(), io.ErrNoProgress) {
		t.Fatalf("non-progressing source accepted: %v", err)
	}
	before := calls
	if _, err := stream.Read(buffer); !errors.Is(err, io.ErrNoProgress) || calls != before {
		t.Fatal("failed source read again", err)
	}
	if err := stream.Close(); err != nil || closes != 1 {
		t.Fatal("close ownership changed", err, closes)
	}
}

func TestBodyProgressResetsEmptyReadBudget(t *testing.T) {
	calls := 0
	stream, err := ownedstream.New(t.Context(), body{read: func(p []byte) (int, error) {
		calls++
		if calls%100 != 0 {
			return 0, nil
		}
		p[0] = 'x'
		if calls == 200 {
			return 1, io.EOF
		}
		return 1, nil
	}, close: func() error { return nil }}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil || string(data) != "xx" {
		t.Fatalf("progressing body failed: %q %v", data, err)
	}
}

func BenchmarkBoundedBodyRead(b *testing.B) {
	text := strings.Repeat("x", 64<<10)
	buffer := make([]byte, 32<<10)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		stream, err := ownedstream.New(b.Context(), io.NopCloser(strings.NewReader(text)), int64(len(text)), int64(len(text)))
		if err != nil {
			b.Fatal(err)
		}
		if _, err = io.CopyBuffer(io.Discard, stream, buffer); err != nil {
			b.Fatal(err)
		}
		if err = stream.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
