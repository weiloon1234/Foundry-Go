// Package ownedstream retains a borrowed stream until reads and close actually
// return. It is used by both sides of one outbound HTTP transport attempt.
package ownedstream

import (
	"context"
	"io"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

type Body struct {
	source   io.ReadCloser
	reader   io.Reader
	ctx      context.Context
	maximum  int64
	expected int64
	readMu   sync.Mutex
	read     int64
	failure  error
	closing  atomic.Bool
	once     sync.Once
	closed   chan struct{}
	closeErr error
	watchMu  sync.Mutex
	watch    func() bool
}

func New(ctx context.Context, source io.ReadCloser, maximum, expected int64) (*Body, error) {
	if ctx == nil || Nil(source) || maximum < 0 || expected < -1 {
		return nil, fault.New(fault.Invalid, "invalid owned stream")
	}
	if expected >= 0 {
		maximum = min(maximum, expected)
	}
	b := &Body{source: source, reader: workscope.Reader(ctx, source), ctx: ctx, maximum: maximum, expected: expected, closed: make(chan struct{})}
	b.watchMu.Lock()
	b.watch = context.AfterFunc(ctx, func() { _ = b.Close() })
	b.watchMu.Unlock()
	return b, nil
}

func Nil(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}

func (b *Body) Read(p []byte) (int, error) {
	if b == nil {
		return 0, fault.New(fault.Invalid, "invalid owned stream")
	}
	b.readMu.Lock()
	defer b.readMu.Unlock()
	if b.closing.Load() {
		return 0, fault.New(fault.Closed, "stream is closed")
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.failure != nil {
		return 0, b.failure
	}
	if len(p) == 0 {
		return 0, nil
	}
	allowed := int(min(int64(len(p)), b.maximum-b.read+1))
	var n int
	var readErr error
	err := callback.Isolated("stream read", func() error { n, readErr = b.reader.Read(p[:allowed]); return nil })
	if err != nil {
		b.failure = fault.Wrap(fault.Internal, "stream read failed", err)
		return 0, b.failure
	}
	if n < 0 || n > allowed || int64(n) > b.maximum-b.read {
		b.failure = fault.New(fault.Invalid, "stream exceeded its byte bound")
		return 0, b.failure
	}
	b.read += int64(n)
	if err := b.ctx.Err(); err != nil {
		b.failure = err
		return n, err
	}
	if readErr == io.EOF && b.expected >= 0 && b.read != b.expected {
		readErr = io.ErrUnexpectedEOF
	}
	if readErr != nil && readErr != io.EOF {
		b.failure = fault.Wrap(fault.Internal, "stream read failed", readErr)
		return n, b.failure
	}
	return n, readErr
}

// Close interrupts a read using the underlying io.ReadCloser contract, then
// waits for that read's actual exit. It never holds readMu while closing source.
func (b *Body) Close() error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		b.closing.Store(true)
		b.watchMu.Lock()
		if b.watch != nil {
			b.watch()
		}
		b.watchMu.Unlock()
		if err := callback.Isolated("stream close", b.source.Close); err != nil {
			b.closeErr = fault.Wrap(fault.Internal, "stream close failed", err)
		}
		b.readMu.Lock()
		b.readMu.Unlock()
		close(b.closed)
	})
	<-b.closed
	return b.closeErr
}

// Err includes a swallowed read failure after a custom transport/callback
// returns. A normal EOF is not a failure.
func (b *Body) Err() error {
	if b == nil {
		return nil
	}
	b.readMu.Lock()
	defer b.readMu.Unlock()
	return b.failure
}
func (b *Body) BytesRead() int64 {
	if b == nil {
		return 0
	}
	b.readMu.Lock()
	defer b.readMu.Unlock()
	return b.read
}
