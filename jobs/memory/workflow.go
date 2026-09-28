package memory

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
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
	members := workflow.Steps()
	completion, hasCompletion := workflow.Completion().Get()
	if hasCompletion {
		members = append(members, completion)
	}
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
	if len(b.workflows) >= b.config.MaxEntries || len(b.entries)+len(items) > b.config.MaxEntries || total > b.config.MaxBytes-b.bytes || uint64(len(items)) > ^uint64(0)-b.sequence {
		return false, fault.New(fault.Conflict, "job workflow capacity reached")
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
	b.workflows[at] = group
	for i, item := range items {
		b.sequence++
		item.sequence = b.sequence
		state := jobs.Waiting
		if (workflow.Kind() == jobs.ChainKind && i > 0) || (hasCompletion && i == len(items)-1) {
			state = jobs.Blocked
		}
		b.transition(item, state, jobs.NoReason, now)
		b.entries[address{key, item.record.Envelope.ID()}] = item
	}
	b.bytes += total
	return true, nil
}
func (b *Backend) advanceWorkflow(item *entry, now time.Time) {
	at := workflowAddress{item.queue, item.record.Workflow}
	group := b.workflows[at]
	if group == nil {
		return
	}
	if item.record.Envelope.ID() == group.completion {
		if group.remaining == 0 {
			group.finished = now
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
			group.finished = now
			return
		}
		completion := b.entries[address{at.queue, group.completion}]
		if completion != nil && completion.record.State.Terminal() {
			group.finished = now
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
	members := append([]jobs.ExecutionID(nil), group.members...)
	if !group.completion.IsZero() {
		members = append(members, group.completion)
	}
	for _, id := range members {
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
