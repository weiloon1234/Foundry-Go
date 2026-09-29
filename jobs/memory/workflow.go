package memory

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/value"
)

type workflowAddress struct {
	queue jobs.Key
	id    jobs.WorkflowID
}
type workflowEntry struct {
	fingerprint [32]byte
	kind        jobs.WorkflowKind
	members     []jobs.ExecutionID
	completion  jobs.ExecutionID
	catch       jobs.ExecutionID
	finally     jobs.ExecutionID
	remaining   int
	failed      bool
	cancelling  bool
	finished    time.Time
	bytes       int64
}

func (b *Backend) JobWorkflow(ctx context.Context, key jobs.Key, workflow jobs.WorkflowEnvelope) (bool, error) {
	if err := workflow.Validate(); err != nil {
		return false, err
	}
	if workflow.Queue() != key.Queue() {
		return false, fault.New(fault.Invalid, "workflow belongs to another queue")
	}
	data, err := workflow.MarshalJSON()
	if err != nil {
		return false, err
	}
	fingerprint := sha256.Sum256(data)
	members := workflow.Members()
	completion, hasCompletion := workflow.Completion().Get()
	items := make([]*entry, len(members))
	total := int64(256 + len(members)*32)
	for i, envelope := range members {
		data, err := envelope.MarshalJSON()
		if err != nil {
			return false, err
		}
		size := int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))
		items[i] = &entry{queue: key, encoded: string(data), bytes: size, record: jobs.Record{Envelope: envelope, Workflow: workflow.ID(), Position: uint32(i)}}
		total += size
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	at := workflowAddress{key, workflow.ID()}
	if previous, ok := b.workflows[at]; ok {
		if previous.fingerprint != fingerprint {
			return false, fault.New(fault.Conflict, "workflow identity already belongs to different members")
		}
		return false, nil
	}
	active := 0
	for _, group := range b.workflows {
		if group.finished.IsZero() {
			active++
		}
	}
	if uint64(len(items)) > ^uint64(0)-b.sequence {
		return false, fault.New(fault.Conflict, "memory job sequence exhausted")
	}
	if active >= b.config.MaxEntries || b.live+len(items) > b.config.MaxEntries || total > b.config.MaxBytes-b.liveBytes {
		return false, jobs.ErrQueueFull
	}
	for _, item := range items {
		if _, exists := b.entries[address{key, item.record.Envelope.ID()}]; exists {
			return false, fault.New(fault.Conflict, "workflow job identity already exists")
		}
		available := item.record.Envelope.AvailableAt()
		if available.IsZero() {
			available = now
		}
		if available.After(now.Add(jobs.MaxDelay)) {
			return false, fault.New(fault.Invalid, "workflow schedule exceeds maximum delay")
		}
		item.record.CreatedAt = now
		item.record.AvailableAt = available
	}
	group := &workflowEntry{fingerprint: fingerprint, kind: workflow.Kind(), remaining: len(workflow.Steps()), bytes: int64(256 + len(members)*32)}
	for _, envelope := range workflow.Steps() {
		group.members = append(group.members, envelope.ID())
	}
	if hasCompletion {
		group.completion = completion.ID()
	}
	if catch, ok := workflow.Catch().Get(); ok {
		group.catch = catch.ID()
	}
	if finally, ok := workflow.Finally().Get(); ok {
		group.finally = finally.ID()
	}
	b.workflows[at] = group
	for i, item := range items {
		b.sequence++
		item.sequence = b.sequence
		state := jobs.Waiting
		if (workflow.Kind() == jobs.ChainKind && i > 0) || i >= len(group.members) {
			// Later chain steps, the completion and callbacks wait for others.
			state = jobs.Blocked
		}
		b.transition(item, state, jobs.NoReason, now)
		b.entries[address{key, item.record.Envelope.ID()}] = item
	}
	b.live += len(items)
	b.liveBytes += total
	b.signal(key)
	return true, nil
}
func (b *Backend) advanceWorkflow(item *entry, now time.Time) {
	at := workflowAddress{item.queue, item.record.Workflow}
	group := b.workflows[at]
	if group == nil {
		return
	}
	id := item.record.Envelope.ID()
	if id == group.catch || id == group.finally {
		if b.settled(at, group) {
			b.settle(at, group, now)
		}
		return
	}
	if id == group.completion {
		if group.remaining == 0 {
			b.settle(at, group, now)
		}
		return
	}
	group.remaining--
	if item.record.State != jobs.Succeeded {
		group.failed = true
	}
	if group.kind == jobs.ChainKind {
		next := int(item.record.Position) + 1
		if next < len(group.members) {
			child := b.entries[address{at.queue, group.members[next]}]
			if child != nil && child.record.State == jobs.Blocked {
				if group.failed || group.cancelling {
					b.transition(child, jobs.Cancelled, jobs.DependencyFailed, now)
				} else {
					b.transition(child, jobs.Waiting, jobs.NoReason, now)
				}
			}
		}
	}
	if group.remaining == 0 {
		if group.completion.IsZero() {
			b.settle(at, group, now)
			return
		}
		completion := b.entries[address{at.queue, group.completion}]
		if completion != nil && completion.record.State.Terminal() {
			b.settle(at, group, now)
			return
		}
		if completion != nil && completion.record.State == jobs.Blocked {
			if group.failed || group.cancelling {
				b.transition(completion, jobs.Cancelled, jobs.DependencyFailed, now)
			} else {
				b.transition(completion, jobs.Waiting, jobs.NoReason, now)
			}
		}
	}
}

// settled reports whether every member and the completion are terminal.
func (b *Backend) settled(at workflowAddress, group *workflowEntry) bool {
	if group.remaining != 0 {
		return false
	}
	if group.completion.IsZero() {
		return true
	}
	completion := b.entries[address{at.queue, group.completion}]
	return completion == nil || completion.record.State.Terminal()
}

// settle releases or cancels the callbacks of a settled workflow and marks it
// finished once they are terminal too.
func (b *Backend) settle(at workflowAddress, group *workflowEntry, now time.Time) {
	pending := false
	for _, callback := range []struct {
		id  jobs.ExecutionID
		run bool
	}{{group.catch, group.failed && !group.cancelling}, {group.finally, !group.cancelling}} {
		if callback.id.IsZero() {
			continue
		}
		item := b.entries[address{at.queue, callback.id}]
		if item == nil {
			continue
		}
		if item.record.State == jobs.Blocked {
			if callback.run {
				b.transition(item, jobs.Waiting, jobs.NoReason, now)
			} else {
				b.transition(item, jobs.Cancelled, jobs.NotTriggered, now)
			}
		}
		if !item.record.State.Terminal() {
			pending = true
		}
	}
	if !pending && group.finished.IsZero() {
		// A finished group's metadata is retained, not live, work.
		group.finished = now
		b.liveBytes -= group.bytes
		b.retainedBytes += group.bytes
	}
}

// workflowMembers lists every job ID of a group, callbacks included.
func (group *workflowEntry) workflowMembers() []jobs.ExecutionID {
	members := append([]jobs.ExecutionID(nil), group.members...)
	for _, id := range []jobs.ExecutionID{group.completion, group.catch, group.finally} {
		if !id.IsZero() {
			members = append(members, id)
		}
	}
	return members
}

// JobWorkflowStatus reports one workflow's progress without payloads.
func (b *Backend) JobWorkflowStatus(ctx context.Context, key jobs.Key, id jobs.WorkflowID) (value.Optional[jobs.WorkflowStatus], error) {
	if id.IsZero() {
		return value.Optional[jobs.WorkflowStatus]{}, fault.New(fault.Invalid, "workflow status requires an identity")
	}
	_, release, err := b.begin(ctx, key)
	if err != nil {
		return value.Optional[jobs.WorkflowStatus]{}, err
	}
	defer release()
	group := b.workflows[workflowAddress{key, id}]
	if group == nil {
		return value.Optional[jobs.WorkflowStatus]{}, nil
	}
	status := jobs.WorkflowStatus{ID: id, Kind: group.kind, Failed: group.failed, Cancelling: group.cancelling, FinishedAt: group.finished}
	counted := append([]jobs.ExecutionID(nil), group.members...)
	if !group.completion.IsZero() {
		counted = append(counted, group.completion)
	}
	for _, member := range counted {
		item := b.entries[address{key, member}]
		if item == nil {
			continue
		}
		status.Count(item.record.State)
	}
	return value.Set(status), nil
}

func (b *Backend) JobCancelWorkflow(ctx context.Context, key jobs.Key, id jobs.WorkflowID) (bool, error) {
	if id.IsZero() {
		return false, fault.New(fault.Invalid, "workflow cancellation requires an identity")
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	group := b.workflows[workflowAddress{key, id}]
	if group == nil || !group.finished.IsZero() {
		return false, nil
	}
	group.cancelling = true
	for _, id := range group.workflowMembers() {
		item := b.entries[address{key, id}]
		if item == nil || item.record.State.Terminal() {
			continue
		}
		item.record.CancellationRequested = true
		if item.record.State == jobs.Waiting || item.record.State == jobs.Blocked {
			b.transition(item, jobs.Cancelled, jobs.CancelRequested, now)
		}
	}
	return true, nil
}
