// Package httpcapture owns the bounded, in-memory HTTP capture handoff. It does
// no I/O and starts no goroutines. Only framework packages can submit captures.
package httpcapture

import (
	"fmt"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const (
	ResponseBytes = 4 << 10
	// MetadataBytes includes bounded strings, header entries, slice backing
	// arrays and snapshot bookkeeping, as well as the response head and the
	// bounded initial preread buffer during its one growth allocation.
	MetadataBytes = 96 << 10
)

// Config contains operational limits only; it cannot select paths or body mode.
type Config struct {
	BodyBytes     int
	RetainedBytes int64
	QueueSize     int
	Rate          int
	Burst         int
}

func DefaultConfig() Config {
	return Config{BodyBytes: 2 << 20, RetainedBytes: 64 << 20, QueueSize: 256, Rate: 50, Burst: 50}
}

// Validate checks every limit against its hard cap. The retained budget must
// admit at least one whole reservation.
func (c Config) Validate() error {
	if c.BodyBytes < 1<<10 || c.BodyBytes > 8<<20 || c.QueueSize < 1 || c.QueueSize > 256 || c.Rate < 1 || c.Rate > 10000 || c.Burst < 1 || c.Burst > 10000 || c.RetainedBytes < int64(c.BodyBytes+1+MetadataBytes) || c.RetainedBytes > 1<<30 {
		return safeerror.NewFault(fault.Invalid, "invalid HTTP capture limits")
	}
	return nil
}

type DropReason uint8

const (
	DropSealed DropReason = iota
	DropRate
	DropCapacity
	DropBytes
	// DropWrite and DropShutdown are reported by the persistence owner after
	// it relinquishes records it could not store.
	DropWrite
	DropShutdown
	DropReasons
)

// Recorder reserves both a slot and bytes before HTTP allocates capture data.
// Dequeued snapshots remain charged until their consumer releases them.
type Recorder struct {
	config        Config
	mu            sync.Mutex
	sealed        bool
	active        int
	retained      int64
	tokens        float64
	last          time.Time
	drops         [DropReasons]uint64
	droppedBefore uint64
	queue         chan *Record
	ready         chan struct{}
	done          chan struct{}
	doneClosed    bool
}

func New(config Config) (*Recorder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Recorder{config: config, tokens: float64(config.Burst), last: time.Now(), queue: make(chan *Record, config.QueueSize), ready: make(chan struct{}, 1), done: make(chan struct{})}, nil
}

func (r *Recorder) BodyBytes() int { return r.config.BodyBytes }

// Reserve never waits for capacity, a consumer, or I/O.
func (r *Recorder) Reserve() *Reservation { return r.reserve(time.Now()) }

func (r *Recorder) reserve(now time.Time) *Reservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	cost := int64(r.config.BodyBytes + 1 + MetadataBytes)
	switch {
	case r.sealed:
		r.drop(DropSealed)
		return nil
	case r.active == r.config.QueueSize:
		r.drop(DropCapacity)
		return nil
	case cost > r.config.RetainedBytes-r.retained:
		r.drop(DropBytes)
		return nil
	}
	r.tokens = min(float64(r.config.Burst), r.tokens+max(0, now.Sub(r.last).Seconds())*float64(r.config.Rate))
	r.last = now
	if r.tokens < 1 {
		r.drop(DropRate)
		return nil
	}
	r.tokens--
	r.active++
	r.retained += cost
	return &Reservation{recorder: r, cost: cost}
}

func (r *Recorder) drop(reason DropReason) { r.drops[reason]++; r.droppedBefore++ }

// Lost counts captures the consumer could not store, including the loss
// counts those records carried, so the next completed record reports them.
func (r *Recorder) Lost(reason DropReason, records, carried uint64) {
	if records == 0 && carried == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drops[reason] += records
	r.droppedBefore += records + carried
}

// Drops returns owned cumulative counters indexed by DropReason.
func (r *Recorder) Drops() [DropReasons]uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drops
}

// Usage includes prereads, queued records and records held by the consumer.
func (r *Recorder) Usage() (int, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active, r.retained
}

// Seal refuses new reservations. Existing reservations may still complete.
func (r *Recorder) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
	r.finishLocked()
}

func (r *Recorder) finishLocked() {
	if r.sealed && r.active == 0 && !r.doneClosed {
		r.doneClosed = true
		close(r.done)
	}
}

func (r *Recorder) Done() <-chan struct{}  { return r.done }
func (r *Recorder) Ready() <-chan struct{} { return r.ready }

// Drain transfers one queued record without waiting. Release it only after all
// persistence/retry work has relinquished its snapshot.
func (r *Recorder) Drain() *Record {
	select {
	case record := <-r.queue:
		return record
	default:
		return nil
	}
}

type Reservation struct {
	recorder *Recorder
	cost     int64
	once     sync.Once
}

// Complete takes exclusive ownership of every snapshot buffer. The HTTP owner
// must never mutate or retain those buffers after this call.
func (l *Reservation) Complete(snapshot Snapshot) {
	l.once.Do(func() {
		r := l.recorder
		// Prefix capacity, rather than length, accounts for a short preread's
		// backing allocation. Metadata has a conservative fixed allowance.
		cost := int64(cap(snapshot.Body.Prefix) + MetadataBytes)
		r.mu.Lock()
		r.retained -= l.cost - cost
		snapshot.DroppedBefore = r.droppedBefore
		r.droppedBefore = 0
		// Every active reservation owns a queue slot, including records already
		// drained by a consumer. This send therefore cannot wait for capacity.
		r.queue <- &Record{Snapshot: snapshot, recorder: r, cost: cost}
		r.mu.Unlock()
		select {
		case r.ready <- struct{}{}:
		default:
		}
	})
}

// Record is exclusively owned by the draining consumer, including its buffers.
type Record struct {
	Snapshot Snapshot
	recorder *Recorder
	cost     int64
	once     sync.Once
}

func (r *Record) Release() {
	r.once.Do(func() {
		r.Snapshot = Snapshot{}
		r.recorder.mu.Lock()
		r.recorder.retained -= r.cost
		r.recorder.active--
		r.recorder.finishLocked()
		r.recorder.mu.Unlock()
	})
}

// Capture data must never be disclosed through ordinary diagnostic formatting.
func (Snapshot) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("[HTTP capture redacted]")) }
func (r *Record) Format(s fmt.State, verb rune) { r.Snapshot.Format(s, verb) }
