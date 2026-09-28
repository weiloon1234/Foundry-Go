package workscope

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestReaderProgressAndInvalidCounts(t *testing.T) {
	calls := 0
	r := Reader(t.Context(), readerFunc(func(p []byte) (int, error) {
		calls++
		if calls == 100 {
			p[0] = 'x'
			return 1, nil
		}
		if calls > 200 {
			return 0, io.EOF
		}
		return 0, nil
	}))
	data, err := io.ReadAll(r)
	if !errors.Is(err, io.ErrNoProgress) || string(data) != "x" || calls != 200 {
		t.Fatal("progress must reset the consecutive empty-read bound", calls, err)
	}
	for _, n := range []int{-1, 2} {
		r := Reader(t.Context(), readerFunc(func([]byte) (int, error) { return n, nil }))
		if read, err := r.Read(make([]byte, 1)); read != 0 || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid count escaped the borrowed reader", read, err)
		}
	}
}

func TestReaderCancellationDuringCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	r := Reader(ctx, readerFunc(func(p []byte) (int, error) {
		calls++
		p[0] = 'x'
		cancel()
		return 1, io.EOF
	}))
	if n, err := r.Read(make([]byte, 1)); n != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("reader's terminal result hid cancellation", n, err)
	}
	if n, err := r.Read(make([]byte, 1)); n != 0 || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("canceled reader called the source again", n, err)
	}
}
