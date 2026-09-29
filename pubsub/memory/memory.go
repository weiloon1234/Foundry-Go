// Package memory provides explicit local ephemeral fan-out for pub/sub contracts.
package memory

import (
	"context"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/pubsubstream"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// Backend owns a bounded number of local subscriptions. It starts no goroutines,
// does not share process-global state, and never becomes a Redis failure fallback.
type Backend struct {
	mu       sync.Mutex
	capacity int
	closed   bool
	streams  map[*stream]struct{}
}

func New(maxSubscriptions int) (*Backend, error) {
	if maxSubscriptions <= 0 {
		return nil, fault.New(fault.Invalid, "memory pub/sub capacity must be positive")
	}
	return &Backend{capacity: maxSubscriptions, streams: make(map[*stream]struct{})}, nil
}

var _ pubsub.Backend = (*Backend)(nil)

// FoundryAdapter marks the backend as framework-owned adapter I/O.
func (*Backend) FoundryAdapter(frameworkadapter.Seal) {}

func (b *Backend) Publish(ctx context.Context, channel pubsub.Channel, data []byte) (uint64, error) {
	if err := pubsub.ValidatePublish(ctx, channel, data); err != nil {
		return 0, err
	}
	if b == nil || b.streams == nil {
		return 0, fault.New(fault.Invalid, "memory pub/sub adapter is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, pubsub.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var count uint64
	for s := range b.streams {
		if !s.channels[channel] {
			continue
		}
		count++
		if err := s.buffer.Push(pubsub.Message{Channel: channel, Data: data}); err != nil {
			delete(b.streams, s)
		}
	}
	return count, nil
}
func (b *Backend) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	if err := pubsub.ValidateSubscribe(ctx, channels, limits); err != nil {
		return nil, err
	}
	if b == nil || b.streams == nil {
		return nil, fault.New(fault.Invalid, "memory pub/sub adapter is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, pubsub.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A live-subscription bound fails fast; it is retryable once one closes.
	if len(b.streams) >= b.capacity {
		return nil, fault.New(fault.Overloaded, "memory pub/sub subscription capacity reached")
	}
	buffer, err := pubsubstream.New(limits)
	if err != nil {
		return nil, err
	}
	s := &stream{backend: b, buffer: buffer, channels: make(map[pubsub.Channel]bool, len(channels))}
	for _, channel := range channels {
		s.channels[channel] = true
	}
	b.streams[s] = struct{}{}
	return s, nil
}
func (b *Backend) Close() error {
	if b == nil || b.streams == nil {
		return fault.New(fault.Invalid, "memory pub/sub adapter is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.streams {
		s.buffer.Finish(pubsub.ErrClosed)
		delete(b.streams, s)
	}
	return nil
}

type stream struct {
	backend  *Backend
	buffer   *pubsubstream.Buffer
	channels map[pubsub.Channel]bool
}

// FoundryAdapter marks the stream as framework-owned adapter I/O.
func (*stream) FoundryAdapter(frameworkadapter.Seal) {}

func (s *stream) Next(ctx context.Context) (pubsub.Message, error) { return s.buffer.Next(ctx) }
func (s *stream) Done() <-chan struct{}                            { return s.buffer.Done() }
func (s *stream) Err() error                                       { return s.buffer.Err() }
func (s *stream) Close(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "pub/sub stream close needs context")
	}
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	s.buffer.Finish(pubsub.ErrClosed)
	delete(s.backend.streams, s)
	return nil
}
