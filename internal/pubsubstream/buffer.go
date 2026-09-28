// Package pubsubstream owns bounded, loss-reporting adapter delivery queues.
package pubsubstream

import (
	"context"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// Buffer stores owned message snapshots and never silently evicts queued payloads.
// Finish is terminal and discards pending data; callers can observe the loss error.
type Buffer struct {
	mu                sync.Mutex
	limits            pubsub.Limits
	entries           []pubsub.Message
	head, size, bytes int
	err               error
	wake, done        chan struct{}
}

func New(limits pubsub.Limits) (*Buffer, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Buffer{limits: limits, entries: make([]pubsub.Message, limits.Messages), wake: make(chan struct{}, 1), done: make(chan struct{})}, nil
}
func (b *Buffer) Push(message pubsub.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b.err
	}
	if len(message.Data) == 0 || len(message.Data) > b.limits.PayloadBytes {
		b.finishLocked(fault.New(fault.Invalid, "pub/sub incoming payload exceeds its bound"))
		return b.err
	}
	if b.size == len(b.entries) || len(message.Data) > b.limits.Bytes-b.bytes {
		b.finishLocked(pubsub.ErrOverflow)
		return b.err
	}
	message.Data = slices.Clone(message.Data)
	b.entries[(b.head+b.size)%len(b.entries)] = message
	b.size++
	b.bytes += len(message.Data)
	select {
	case b.wake <- struct{}{}:
	default:
	}
	return nil
}
func (b *Buffer) finishLocked(err error) {
	if b.err != nil {
		return
	}
	if err == nil {
		err = pubsub.ErrClosed
	}
	b.err = err
	clear(b.entries)
	b.size = 0
	b.bytes = 0
	close(b.done)
}
func (b *Buffer) Finish(err error)      { b.mu.Lock(); defer b.mu.Unlock(); b.finishLocked(err) }
func (b *Buffer) Done() <-chan struct{} { return b.done }
func (b *Buffer) Err() error            { b.mu.Lock(); defer b.mu.Unlock(); return b.err }
func (b *Buffer) Next(ctx context.Context) (pubsub.Message, error) {
	if ctx == nil {
		return pubsub.Message{}, fault.New(fault.Invalid, "pub/sub receive needs context")
	}
	for {
		b.mu.Lock()
		if b.err != nil {
			err := b.err
			b.mu.Unlock()
			return pubsub.Message{}, err
		}
		if err := ctx.Err(); err != nil {
			b.mu.Unlock()
			return pubsub.Message{}, err
		}
		if b.size > 0 {
			message := b.entries[b.head]
			b.entries[b.head] = pubsub.Message{}
			b.head = (b.head + 1) % len(b.entries)
			b.size--
			b.bytes -= len(message.Data)
			b.mu.Unlock()
			return message, nil
		}
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return pubsub.Message{}, ctx.Err()
		case <-b.done:
		case <-b.wake:
		}
	}
}
