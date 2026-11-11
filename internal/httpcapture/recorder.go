// Package httpcapture owns the bounded, in-memory HTTP capture handoff for
// both directions: inbound webhook requests and outbound client attempts. It
// does no I/O and starts no goroutines. Only framework packages can submit
// captures.
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
	// BodyBytes bounds one captured body.
	BodyBytes     int
	RetainedBytes int64
	QueueSize     int
	// Rate and Burst admit captures per lane.
	Rate  int
	Burst int
	// LaneShare divides QueueSize and RetainedBytes among lanes: one lane may
	// hold at most 1/LaneShare of either (at least one row). Zero means one.
	LaneShare int
}

func DefaultConfig() Config {
	return Config{BodyBytes: 2 << 20, RetainedBytes: 64 << 20, QueueSize: 256, Rate: 50, Burst: 50, LaneShare: 1}
}

// Validate checks every limit against its hard cap. One lane's share of the
// retained budget must admit at least one whole body with its metadata.
func (c Config) Validate() error {
	share := int64(max(c.LaneShare, 1))
	if c.LaneShare < 0 || c.LaneShare > 64 || c.BodyBytes < 1<<10 || c.BodyBytes > 8<<20 || c.QueueSize < 1 || c.QueueSize > 256 || c.Rate < 1 || c.Rate > 10000 || c.Burst < 1 || c.Burst > 10000 || c.RetainedBytes/share < int64(c.BodyBytes+1+MetadataBytes) || c.RetainedBytes > 1<<30 {
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
// Dequeued snapshots remain charged until their consumer releases them. Every
// producer admits through its own Lane; the recorder holds only the process
// caps, the drop counters and the queue.
type Recorder[S any] struct {
	config     Config
	laneRows   int
	laneBytes  int64
	mu         sync.Mutex
	sealed     bool
	active     int
	retained   int64
	drops      [DropReasons]uint64
	queue      chan *Record[S]
	ready      chan struct{}
	done       chan struct{}
	doneClosed bool
}

func New[S any](config Config) (*Recorder[S], error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	share := max(config.LaneShare, 1)
	return &Recorder[S]{config: config, laneRows: max(1, config.QueueSize/share), laneBytes: config.RetainedBytes / int64(share), queue: make(chan *Record[S], config.QueueSize), ready: make(chan struct{}, 1), done: make(chan struct{})}, nil
}

func (r *Recorder[S]) BodyBytes() int { return r.config.BodyBytes }

// NewLane returns one producer's admission state: its own token bucket, its
// share of rows and bytes, and the loss count its next record carries.
func (r *Recorder[S]) NewLane() *Lane[S] {
	return &Lane[S]{recorder: r, tokens: float64(r.config.Burst), last: time.Now()}
}

// Drops returns owned cumulative counters indexed by DropReason.
func (r *Recorder[S]) Drops() [DropReasons]uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drops
}

// Usage includes active reservations, queued records and records held by the consumer.
func (r *Recorder[S]) Usage() (int, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active, r.retained
}

// Seal refuses new reservations. Existing reservations may still grow and complete.
func (r *Recorder[S]) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
	r.finishLocked()
}

func (r *Recorder[S]) finishLocked() {
	if r.sealed && r.active == 0 && !r.doneClosed {
		r.doneClosed = true
		close(r.done)
	}
}

func (r *Recorder[S]) Done() <-chan struct{}  { return r.done }
func (r *Recorder[S]) Ready() <-chan struct{} { return r.ready }

// Drain transfers one queued record without waiting. Release it only after all
// persistence/retry work has relinquished its snapshot.
func (r *Recorder[S]) Drain() *Record[S] {
	select {
	case record := <-r.queue:
		return record
	default:
		return nil
	}
}

// Lane is one producer's admission state. Inbound capture has one lane;
// outbound capture has one per client name, so a busy client spends only its
// own tokens and share and its losses reach only its own next record.
type Lane[S any] struct {
	recorder      *Recorder[S]
	tokens        float64
	last          time.Time
	active        int
	retained      int64
	droppedBefore uint64
}

// Reserve never waits for capacity, a consumer, or I/O. initial is charged
// now; Reservation.Grow charges more later.
func (l *Lane[S]) Reserve(initial int64) *Reservation[S] { return l.reserve(time.Now(), initial) }

func (l *Lane[S]) reserve(now time.Time, initial int64) *Reservation[S] {
	r := l.recorder
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.sealed:
		l.drop(DropSealed)
		return nil
	case r.active == r.config.QueueSize || l.active == r.laneRows:
		l.drop(DropCapacity)
		return nil
	case initial > r.config.RetainedBytes-r.retained || initial > r.laneBytes-l.retained:
		l.drop(DropBytes)
		return nil
	}
	l.tokens = min(float64(r.config.Burst), l.tokens+max(0, now.Sub(l.last).Seconds())*float64(r.config.Rate))
	l.last = now
	if l.tokens < 1 {
		l.drop(DropRate)
		return nil
	}
	l.tokens--
	l.charge(1, initial)
	return &Reservation[S]{lane: l, cost: initial}
}

// drop and charge require the recorder lock.
func (l *Lane[S]) drop(reason DropReason) { l.recorder.drops[reason]++; l.droppedBefore++ }
func (l *Lane[S]) charge(rows int, bytes int64) {
	l.active += rows
	l.retained += bytes
	l.recorder.active += rows
	l.recorder.retained += bytes
}

// Reservation is one admitted capture before it is queued. Only its producer
// uses it.
type Reservation[S any] struct {
	lane *Lane[S]
	cost int64
	done bool
}

// Grow charges n more bytes against the lane share and the process budget.
// false means the caller must stop buffering; nothing waits and no record is
// lost.
func (v *Reservation[S]) Grow(n int64) bool {
	if v == nil || n < 0 {
		return false
	}
	l := v.lane
	r := l.recorder
	r.mu.Lock()
	defer r.mu.Unlock()
	if v.done || n > r.config.RetainedBytes-r.retained || n > r.laneBytes-l.retained {
		return false
	}
	v.cost += n
	l.charge(0, n)
	return true
}

// Complete takes exclusive ownership of every snapshot buffer and settles the
// charge to retained, which never exceeds what was admitted. The producer must
// never mutate or retain those buffers after this call.
func (v *Reservation[S]) Complete(snapshot S, retained int64) {
	if v == nil {
		return
	}
	l := v.lane
	r := l.recorder
	r.mu.Lock()
	if v.done {
		r.mu.Unlock()
		return
	}
	v.done = true
	retained = min(max(retained, 0), v.cost)
	l.charge(0, retained-v.cost)
	record := &Record[S]{Snapshot: snapshot, DroppedBefore: l.droppedBefore, lane: l, cost: retained}
	l.droppedBefore = 0
	// Every active reservation owns a queue slot, including records already
	// drained by a consumer. This send therefore cannot wait for capacity.
	r.queue <- record
	r.mu.Unlock()
	select {
	case r.ready <- struct{}{}:
	default:
	}
}

// Record is exclusively owned by the draining consumer, including its buffers.
// DroppedBefore counts captures its lane lost after the lane's previous record.
type Record[S any] struct {
	Snapshot      S
	DroppedBefore uint64
	lane          *Lane[S]
	cost          int64
	released      bool
}

// Release frees a stored record.
func (r *Record[S]) Release() { r.settle(false, 0) }

// Lose frees a record the consumer could not store. It counts the record and
// the loss it carried back to its lane, so the lane's next record reports them.
func (r *Record[S]) Lose(reason DropReason) { r.settle(true, reason) }

func (r *Record[S]) settle(lost bool, reason DropReason) {
	l := r.lane
	recorder := l.recorder
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	if lost {
		recorder.drops[reason]++
		l.droppedBefore += 1 + r.DroppedBefore
	}
	var zero S
	r.Snapshot = zero
	l.charge(-1, -r.cost)
	recorder.finishLocked()
}

// Capture data must never be disclosed through ordinary diagnostic formatting.
func (Snapshot) Format(s fmt.State, _ rune)        { _, _ = s.Write([]byte("[HTTP capture redacted]")) }
func (*Record[S]) Format(s fmt.State, _ rune)      { _, _ = s.Write([]byte("[HTTP capture redacted]")) }
func (*Reservation[S]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("[HTTP capture redacted]")) }
