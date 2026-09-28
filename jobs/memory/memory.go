// Package memory implements a bounded, deterministic single-process job
// authority. It is useful for tests and explicitly local work, not durability.
// It never silently evicts unfinished work or falls back from another backend.
package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Config bounds all retained records and transitions. Retention applies after
// terminal completion, preserving dispatch deduplication for that window.
// Clock belongs to the authority; a shared test clock makes expiry deterministic.
type Config struct {
	jobs.QueueConfig
	Clock clock.Clock
}

func DefaultConfig() Config {
	return Config{QueueConfig: jobs.DefaultQueueConfig(), Clock: clock.System{}}
}
func (c Config) Validate() error {
	if err := c.QueueConfig.Validate(); err != nil {
		return err
	}
	if c.Clock == nil {
		return fault.New(fault.Invalid, "memory job backend requires a clock")
	}
	return nil
}

type address struct {
	queue jobs.Key
	id    jobs.ExecutionID
}
type uniqueAddress struct {
	queue  jobs.Key
	digest string
}
type entry struct {
	queue    jobs.Key
	record   jobs.Record
	encoded  string
	owner    lease.Owner
	sequence uint64
	bytes    int64
}

// Backend owns no goroutines and never invokes application callbacks under its
// mutex except the injected Clock. A supplied Clock must be concurrency-safe and must not reenter Backend.
type Backend struct {
	workflows map[workflowAddress]*workflowEntry
	mu        sync.Mutex
	config    Config
	entries   map[address]*entry
	unique    map[uniqueAddress]time.Time
	sequence  uint64
	bytes     int64
	lastTime  time.Time
	closed    bool
}

func New(config Config) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{config: config, workflows: make(map[workflowAddress]*workflowEntry), entries: make(map[address]*entry), unique: make(map[uniqueAddress]time.Time)}, nil
}

var _ jobs.Backend = (*Backend)(nil)

func (b *Backend) begin(ctx context.Context, key jobs.Key) (time.Time, func(), error) {
	if err := jobs.ValidateOperation(ctx, key); err != nil {
		return time.Time{}, nil, err
	}
	if b == nil || b.entries == nil {
		return time.Time{}, nil, fault.New(fault.Invalid, "memory job backend is not initialized")
	}
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
		b.mu.Unlock()
		return time.Time{}, nil, err
	}
	if b.closed {
		b.mu.Unlock()
		return time.Time{}, nil, fault.New(fault.Closed, "memory job backend is closed")
	}
	now := b.config.Clock.Now()
	if now.Before(b.lastTime) {
		now = b.lastTime
	} else {
		b.lastTime = now
	}
	b.reap(now)
	return now, b.mu.Unlock, nil
}
func (b *Backend) reap(now time.Time) {
	for key, expiry := range b.unique {
		if !now.Before(expiry) {
			delete(b.unique, key)
		}
	}
	for key, item := range b.entries {
		r := &item.record
		if r.State.Terminal() {
			finished := r.FinishedAt
			if !r.Workflow.IsZero() {
				group := b.workflows[workflowAddress{key.queue, r.Workflow}]
				if group == nil || group.finished.IsZero() {
					continue
				}
				finished = group.finished
			}
			if !now.Before(finished.Add(b.config.Retention)) {
				b.bytes -= item.bytes
				delete(b.entries, key)
			}
			continue
		}
		if (r.State == jobs.Reserved || r.State == jobs.Running) && !now.Before(r.LeaseExpiresAt) {
			switch {
			case r.CancellationRequested:
				b.transition(item, jobs.Cancelled, jobs.CancelRequested, now)
			case r.Attempts >= r.Envelope.Policy().Attempts:
				b.transition(item, jobs.Failed, jobs.AttemptLimit, now)
			default:
				r.AvailableAt = now
				b.transition(item, jobs.Waiting, jobs.LeaseExpired, now)
			}
		}
	}
	for key, group := range b.workflows {
		if !group.finished.IsZero() && !now.Before(group.finished.Add(b.config.Retention)) {
			b.bytes -= group.bytes
			delete(b.workflows, key)
		}
	}
}
func (b *Backend) transition(item *entry, state jobs.State, reason jobs.Reason, now time.Time) {
	wasTerminal := item.record.State.Terminal()
	r := &item.record
	r.State = state
	if state != jobs.Reserved && state != jobs.Running {
		item.owner = lease.Owner{}
		r.LeaseExpiresAt = time.Time{}
	}
	if state.Terminal() {
		r.FinishedAt = now
	}
	transition := jobs.Transition{State: state, At: now, Attempt: r.Attempts, Reason: reason}
	if len(r.History) == b.config.MaxHistory {
		copy(r.History, r.History[1:])
		r.History[len(r.History)-1] = transition
	} else {
		r.History = append(r.History, transition)
	}
	if state.Terminal() && !wasTerminal && !r.Workflow.IsZero() {
		b.advanceWorkflow(item, now)
	}
}
func (b *Backend) owned(key jobs.Key, proof jobs.Ownership) (*entry, bool) {
	item, ok := b.entries[address{key, proof.ID()}]
	return item, ok && (item.record.State == jobs.Reserved || item.record.State == jobs.Running) && item.owner == proof.Owner()
}
func (b *Backend) JobEnqueue(ctx context.Context, key jobs.Key, envelope jobs.Envelope) (bool, error) {
	if err := jobs.ValidateEnqueue(ctx, key, envelope); err != nil {
		return false, err
	}
	data, err := envelope.MarshalJSON()
	if err != nil {
		return false, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	at := address{key, envelope.ID()}
	if previous, ok := b.entries[at]; ok {
		if previous.encoded != string(data) {
			return false, fault.New(fault.Conflict, "job identity already belongs to a different envelope")
		}
		return false, nil
	}
	unique := envelope.Uniqueness()
	uniqueKey := uniqueAddress{key, unique.Digest}
	if unique.Digest != "" {
		if _, ok := b.unique[uniqueKey]; ok {
			return false, jobs.ErrNotUnique
		}
		if len(b.unique) >= b.config.MaxEntries {
			return false, fault.New(fault.Conflict, "job uniqueness capacity reached")
		}
	}
	size := int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))
	if size > b.config.MaxBytes-b.bytes {
		return false, fault.New(fault.Conflict, "memory job byte capacity reached")
	}
	if len(b.entries) >= b.config.MaxEntries {
		return false, fault.New(fault.Conflict, "memory job capacity reached")
	}
	if b.sequence == ^uint64(0) {
		return false, fault.New(fault.Conflict, "memory job sequence exhausted")
	}
	b.sequence++
	available := envelope.AvailableAt()
	if available.IsZero() {
		available = now
	}
	if available.After(now.Add(jobs.MaxDelay)) {
		return false, fault.New(fault.Invalid, "job schedule exceeds maximum delay")
	}
	item := &entry{queue: key, bytes: size, encoded: string(data), sequence: b.sequence, record: jobs.Record{Envelope: envelope, CreatedAt: now, AvailableAt: available}}
	b.transition(item, jobs.Waiting, jobs.NoReason, now)
	b.entries[at] = item
	if unique.Digest != "" {
		b.unique[uniqueKey] = now.Add(unique.For)
	}
	b.bytes += size
	return true, nil
}
func (b *Backend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	if err := owner.Validate(); err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	if err := lease.ValidateDuration(ttl); err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	defer release()
	var chosen *entry
	for at, item := range b.entries {
		if at.queue != key || item.record.State != jobs.Waiting || item.record.AvailableAt.After(now) {
			continue
		}
		if chosen == nil || item.record.AvailableAt.Before(chosen.record.AvailableAt) || item.record.AvailableAt.Equal(chosen.record.AvailableAt) && item.sequence < chosen.sequence {
			chosen = item
		}
	}
	if chosen == nil {
		return value.Optional[jobs.Reservation]{}, nil
	}
	chosen.owner = owner
	chosen.record.LeaseExpiresAt = now.Add(ttl)
	b.transition(chosen, jobs.Reserved, jobs.NoReason, now)
	proof, err := jobs.NewOwnership(chosen.record.Envelope.ID(), owner)
	if err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	return value.Set(jobs.Reservation{Envelope: chosen.record.Envelope, Ownership: proof, Attempts: chosen.record.Attempts, ExpiresAt: chosen.record.LeaseExpiresAt}), nil
}
func (b *Backend) JobStart(ctx context.Context, key jobs.Key, proof jobs.Ownership) (uint32, error) {
	if err := proof.Validate(); err != nil {
		return 0, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return 0, err
	}
	defer release()
	item, ok := b.owned(key, proof)
	if !ok {
		return 0, jobs.ErrOwnershipLost
	}
	if item.record.CancellationRequested {
		return 0, jobs.ErrCancelled
	}
	if item.record.State == jobs.Running {
		return item.record.Attempts, nil
	}
	if item.record.Attempts >= item.record.Envelope.Policy().Attempts {
		b.transition(item, jobs.Failed, jobs.AttemptLimit, now)
		return 0, jobs.ErrOwnershipLost
	}
	item.record.Attempts++
	b.transition(item, jobs.Running, jobs.NoReason, now)
	return item.record.Attempts, nil
}
func (b *Backend) JobRenew(ctx context.Context, key jobs.Key, proof jobs.Ownership, ttl time.Duration) (jobs.LeaseStatus, error) {
	if err := proof.Validate(); err != nil {
		return jobs.LeaseStatus{}, err
	}
	if err := lease.ValidateDuration(ttl); err != nil {
		return jobs.LeaseStatus{}, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return jobs.LeaseStatus{}, err
	}
	defer release()
	item, ok := b.owned(key, proof)
	if !ok {
		return jobs.LeaseStatus{}, nil
	}
	item.record.LeaseExpiresAt = now.Add(ttl)
	return jobs.LeaseStatus{Owned: true, CancellationRequested: item.record.CancellationRequested}, nil
}
func (b *Backend) JobFinish(ctx context.Context, key jobs.Key, proof jobs.Ownership, result jobs.Result) (bool, error) {
	if err := proof.Validate(); err != nil {
		return false, err
	}
	if err := result.Validate(); err != nil {
		return false, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	item, ok := b.owned(key, proof)
	if !ok {
		return false, nil
	}
	state, reason := result.State, result.Reason
	if item.record.CancellationRequested {
		state, reason = jobs.Cancelled, jobs.CancelRequested
	} else if state == jobs.Waiting && item.record.Attempts >= item.record.Envelope.Policy().Attempts {
		state, reason = jobs.Failed, jobs.AttemptLimit
	}
	if state == jobs.Succeeded && item.record.State != jobs.Running {
		return false, fault.New(fault.Invalid, "unstarted job cannot succeed")
	}
	if state == jobs.Waiting {
		item.record.AvailableAt = now.Add(result.Delay)
	}
	b.transition(item, state, reason, now)
	return true, nil
}
func (b *Backend) JobCancel(ctx context.Context, key jobs.Key, target jobs.Target) (bool, error) {
	if err := target.Validate(); err != nil {
		return false, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	item, ok := b.entries[address{key, target.ID}]
	if !ok || item.record.State.Terminal() || item.record.Envelope.Target() != target {
		return false, nil
	}
	if item.record.CancellationRequested {
		return true, nil
	}
	item.record.CancellationRequested = true
	if item.record.State == jobs.Waiting || item.record.State == jobs.Blocked {
		b.transition(item, jobs.Cancelled, jobs.CancelRequested, now)
	}
	return true, nil
}
func (b *Backend) JobInspect(ctx context.Context, key jobs.Key, id jobs.ExecutionID) (value.Optional[jobs.Record], error) {
	if id.IsZero() {
		return value.Optional[jobs.Record]{}, fault.New(fault.Invalid, "job inspection requires an identity")
	}
	_, release, err := b.begin(ctx, key)
	if err != nil {
		return value.Optional[jobs.Record]{}, err
	}
	defer release()
	item, ok := b.entries[address{key, id}]
	if !ok {
		return value.Optional[jobs.Record]{}, nil
	}
	snapshot := item.record
	snapshot.History = slices.Clone(snapshot.History)
	return value.Set(snapshot), nil
}

// Close invalidates this local authority. Stop/drain workers and callers first;
// unfinished work is intentionally not persisted by the memory adapter.
func (b *Backend) Close() error {
	if b == nil || b.entries == nil {
		return fault.New(fault.Invalid, "memory job backend is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	clear(b.entries)
	clear(b.unique)
	clear(b.workflows)
	b.bytes = 0
	return nil
}
