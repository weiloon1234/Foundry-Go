// Package memory implements a bounded, deterministic single-process job
// authority. It is useful for tests and explicitly local work, not durability.
// It never silently evicts unfinished work or falls back from another backend.
package memory

import (
	"cmp"
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
// Limits apply to the whole instance. Clock belongs to the authority; a shared
// test clock makes expiry deterministic.
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
	queue     jobs.Key
	record    jobs.Record
	encoded   string
	owner     lease.Owner
	sequence  uint64
	bytes     int64
	abandoned uint32
	// uniqueUntil is the uniqueness window this record opened, so an
	// until-processing release never removes a later job's window.
	uniqueUntil time.Time
}

// Backend owns no goroutines and never invokes application callbacks under its
// mutex except the injected Clock. A supplied Clock must be concurrency-safe and must not reenter Backend.
type Backend struct {
	workflows     map[workflowAddress]*workflowEntry
	mu            sync.Mutex
	config        Config
	entries       map[address]*entry
	unique        map[uniqueAddress]time.Time
	sequence      uint64
	live          int
	liveBytes     int64
	retained      int
	retainedBytes int64
	lastTime      time.Time
	wake          map[jobs.Key]chan struct{}
	closed        bool
}

func New(config Config) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{config: config, workflows: make(map[workflowAddress]*workflowEntry), entries: make(map[address]*entry), unique: make(map[uniqueAddress]time.Time)}, nil
}

var _ jobs.Backend = (*Backend)(nil)
var _ jobs.WakeBackend = (*Backend)(nil)
var _ jobs.StatsBackend = (*Backend)(nil)
var _ jobs.ForgetBackend = (*Backend)(nil)
var _ jobs.WorkflowStatusBackend = (*Backend)(nil)

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

// JobWakeup returns a channel closed by the next accepted enqueue, workflow or
// manual retry in key's queue on this instance, so idle workers of that queue
// poll immediately.
func (b *Backend) JobWakeup(key jobs.Key) <-chan struct{} {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wake == nil {
		b.wake = make(map[jobs.Key]chan struct{})
	}
	wake, ok := b.wake[key]
	if !ok {
		wake = make(chan struct{})
		b.wake[key] = wake
	}
	return wake
}
func (b *Backend) signal(key jobs.Key) {
	if wake, ok := b.wake[key]; ok {
		close(wake)
		delete(b.wake, key)
	}
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
				b.remove(key, item)
			}
			continue
		}
		if (r.State == jobs.Reserved || r.State == jobs.Running) && !now.Before(r.LeaseExpiresAt) {
			if r.State == jobs.Reserved {
				item.abandoned++
			}
			switch {
			case r.CancellationRequested:
				b.transition(item, jobs.Cancelled, jobs.CancelRequested, now)
			case r.State == jobs.Reserved && item.abandoned >= max(r.Envelope.Policy().Attempts, 3):
				// Repeated expiry before start (for example a payload that
				// crashes its process while decoding) must not redeliver forever.
				b.transition(item, jobs.Failed, jobs.DeliveryLimit, now)
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
			b.retainedBytes -= group.bytes
			delete(b.workflows, key)
		}
	}
	b.evict()
}

// remove drops one record and its accounting. Callers own workflow integrity.
func (b *Backend) remove(at address, item *entry) {
	if item.record.State.Terminal() {
		b.retained--
		b.retainedBytes -= item.bytes
	} else {
		b.live--
		b.liveBytes -= item.bytes
	}
	delete(b.entries, at)
}

// evict keeps terminal records within MaxRetained and MaxBytes by removing the
// oldest independent terminal records, then the oldest finished workflows.
// Unfinished work is never evicted.
func (b *Backend) evict() {
	limit := b.config.RetainedLimit()
	over := func() bool { return b.retained > limit || b.retainedBytes > b.config.MaxBytes }
	if !over() {
		return
	}
	var candidates []address
	for at, item := range b.entries {
		if item.record.State.Terminal() && item.record.Workflow.IsZero() {
			candidates = append(candidates, at)
		}
	}
	slices.SortFunc(candidates, func(x, y address) int {
		a, c := b.entries[x], b.entries[y]
		return cmp.Or(a.record.FinishedAt.Compare(c.record.FinishedAt), cmp.Compare(a.sequence, c.sequence))
	})
	for _, at := range candidates {
		if !over() {
			return
		}
		b.remove(at, b.entries[at])
	}
	var finished []workflowAddress
	for at, group := range b.workflows {
		if !group.finished.IsZero() {
			finished = append(finished, at)
		}
	}
	slices.SortFunc(finished, func(x, y workflowAddress) int {
		return b.workflows[x].finished.Compare(b.workflows[y].finished)
	})
	for _, at := range finished {
		if !over() {
			return
		}
		group := b.workflows[at]
		for _, id := range group.workflowMembers() {
			if item := b.entries[address{at.queue, id}]; item != nil {
				b.remove(address{at.queue, id}, item)
			}
		}
		b.retainedBytes -= group.bytes
		delete(b.workflows, at)
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
	switch {
	case state.Terminal() && !wasTerminal:
		b.live--
		b.liveBytes -= item.bytes
		b.retained++
		b.retainedBytes += item.bytes
	case !state.Terminal() && wasTerminal:
		b.retained--
		b.retainedBytes -= item.bytes
		b.live++
		b.liveBytes += item.bytes
	}
	transition := jobs.Transition{State: state, At: now, Attempt: r.Attempts, Reason: reason, Retry: r.Retries}
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
			return false, fault.Wrap(fault.Overloaded, "job uniqueness capacity is exhausted", jobs.ErrQueueFull)
		}
	}
	size := int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))
	if size > b.config.MaxBytes-b.liveBytes || b.live >= b.config.MaxEntries {
		return false, jobs.ErrQueueFull
	}
	if b.sequence == ^uint64(0) {
		return false, fault.New(fault.Conflict, "memory job sequence exhausted")
	}
	available := envelope.AvailableAt()
	if available.IsZero() {
		available = now
	}
	if available.After(now.Add(jobs.MaxDelay)) {
		return false, fault.New(fault.Invalid, "job schedule exceeds maximum delay")
	}
	b.sequence++
	item := &entry{queue: key, bytes: size, encoded: string(data), sequence: b.sequence, record: jobs.Record{Envelope: envelope, CreatedAt: now, AvailableAt: available}}
	b.live++
	b.liveBytes += size
	b.transition(item, jobs.Waiting, jobs.NoReason, now)
	b.entries[at] = item
	if unique.Digest != "" {
		item.uniqueUntil = now.Add(unique.For)
		b.unique[uniqueKey] = item.uniqueUntil
	}
	b.signal(key)
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
	return value.Set(jobs.Reservation{Retries: chosen.record.Retries, Envelope: chosen.record.Envelope, Ownership: proof, Attempts: chosen.record.Attempts, Exceptions: chosen.record.Exceptions, ExpiresAt: chosen.record.LeaseExpiresAt}), nil
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
	if unique := item.record.Envelope.Uniqueness(); unique.UntilProcessing && !item.uniqueUntil.IsZero() {
		at := uniqueAddress{key, unique.Digest}
		if expiry, ok := b.unique[at]; ok && expiry.Equal(item.uniqueUntil) {
			delete(b.unique, at)
		}
		item.uniqueUntil = time.Time{}
	}
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
	if result.Refund && item.record.State == jobs.Running && item.record.Attempts > 0 {
		item.record.Attempts--
	}
	if !item.record.CancellationRequested && result.Reason.CountsException() && item.record.State == jobs.Running {
		item.record.Exceptions++
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
	item.abandoned = 0
	b.transition(item, state, reason, now)
	b.evict()
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
		b.evict()
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

// JobStats counts one queue's records by operational state.
func (b *Backend) JobStats(ctx context.Context, key jobs.Key) (jobs.QueueStats, error) {
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return jobs.QueueStats{}, err
	}
	defer release()
	var stats jobs.QueueStats
	for at, item := range b.entries {
		if at.queue != key {
			continue
		}
		switch r := item.record; {
		case r.State == jobs.Waiting && r.AvailableAt.After(now):
			stats.Delayed++
		case r.State == jobs.Waiting:
			stats.Waiting++
		case r.State == jobs.Blocked:
			stats.Blocked++
		case r.State == jobs.Reserved || r.State == jobs.Running:
			stats.Leased++
		default:
			stats.Retained++
			if r.State == jobs.Failed {
				stats.Failed++
			}
		}
	}
	return stats, nil
}

// JobForget removes one retained terminal independent record and its
// deduplication identity. Live and workflow records are never removed.
func (b *Backend) JobForget(ctx context.Context, key jobs.Key, target jobs.Target) (bool, error) {
	if err := target.Validate(); err != nil {
		return false, err
	}
	_, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	at := address{key, target.ID}
	item, ok := b.entries[at]
	if !ok || !item.record.State.Terminal() || !item.record.Workflow.IsZero() || item.record.Envelope.Target() != target {
		return false, nil
	}
	b.remove(at, item)
	return true, nil
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
	b.live, b.liveBytes, b.retained, b.retainedBytes = 0, 0, 0, 0
	for key := range b.wake {
		b.signal(key)
	}
	return nil
}
