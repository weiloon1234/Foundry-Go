package redis

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//go:embed jobs.lua
var jobsScript string

// JobBackend borrows the existing Redis client and owns no connections. Limits
// apply per queue and must match across processes. Accepted work persists until
// terminal retention expires; durability depends on Redis persistence/failover.
// Stop workers and publication before closing the client's owning module.
type JobBackend struct {
	client   *Client
	config   jobs.QueueConfig
	identity string
	limits   string
}

func NewJobBackend(client *Client, config jobs.QueueConfig) (*JobBackend, error) {
	if client == nil || client.done == nil {
		return nil, fault.New(fault.Invalid, "jobs require a prepared Redis client")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	identity, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	limits, err := json.Marshal(map[string]any{
		"entries": config.MaxEntries, "bytes": config.MaxBytes, "history": config.MaxHistory,
		"retention": config.Retention.Milliseconds(), "max_delay": jobs.MaxDelay.Milliseconds(),
		"record_bytes": 2*(jobs.MaxPayloadBytes+64*1024) + config.MaxHistory*256,
		"owner_bytes":  lease.OwnerBytes * 2, "attempts": jobs.MaxAttempts, "scan_limit": jobs.ListScanLimit,
		"workflow_steps": jobs.MaxWorkflowSteps,
	})
	if err != nil {
		return nil, err
	}
	return &JobBackend{client: client, config: config, identity: string(identity), limits: string(limits)}, nil
}

var _ jobs.Backend = (*JobBackend)(nil)

type jobRequest struct {
	After        string            `json:"after"`
	Limit        int               `json:"limit"`
	WorkflowKind jobs.WorkflowKind `json:"workflow_kind,omitempty"`
	Steps        []jobRequest      `json:"steps,omitempty"`
	Completion   string            `json:"completion"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
	GroupBytes   int64             `json:"group_bytes,omitempty"`
	Op           string            `json:"op"`
	Unique       string            `json:"unique"`
	UniqueFor    int64             `json:"unique_for"`
	ID           string            `json:"id,omitempty"`
	Owner        string            `json:"owner,omitempty"`
	TTL          int64             `json:"ttl,omitempty"`
	Envelope     string            `json:"envelope,omitempty"`
	Name         jobs.Name         `json:"name,omitempty"`
	Version      jobs.Version      `json:"version,omitempty"`
	Maximum      uint32            `json:"maximum,omitempty"`
	Available    int64             `json:"available"`
	Bytes        int64             `json:"bytes,omitempty"`
	State        jobs.State        `json:"state,omitempty"`
	Reason       jobs.Reason       `json:"reason"`
	Delay        int64             `json:"delay"`
}
type jobStoredRecord struct {
	Workflow  jobs.WorkflowID `json:"workflow,omitempty"`
	Position  uint32          `json:"position"`
	ID        string          `json:"id"`
	Envelope  string          `json:"envelope"`
	State     jobs.State      `json:"state"`
	Attempts  uint32          `json:"attempts"`
	Available int64           `json:"available"`
	Expiry    int64           `json:"expiry"`
	Created   int64           `json:"created"`
	Finished  int64           `json:"finished"`
	Cancelled bool            `json:"cancelled"`
	History   []struct {
		State   jobs.State  `json:"state"`
		At      int64       `json:"at"`
		Attempt uint32      `json:"attempt"`
		Reason  jobs.Reason `json:"reason"`
	} `json:"history"`
}
type jobReply struct {
	Records   []jobStoredRecord `json:"records"`
	Next      string            `json:"next"`
	Inserted  bool              `json:"inserted"`
	Changed   bool              `json:"changed"`
	Owned     bool              `json:"owned"`
	Cancelled bool              `json:"cancelled"`
	Attempt   uint32            `json:"attempt"`
	Record    *jobStoredRecord  `json:"record"`
}

func jobKeys(key jobs.Key) []string {
	base := key.String()
	return []string{base + ":records", base + ":ready", base + ":leases", base + ":finished", base + ":metadata", base + ":unique", base + ":workflows", base + ":workflow_finished", base + ":index"}
}
func (b *JobBackend) command(ctx context.Context, key jobs.Key, request jobRequest) (jobReply, error) {
	if err := jobs.ValidateOperation(ctx, key); err != nil {
		return jobReply{}, err
	}
	if b == nil || b.client == nil {
		return jobReply{}, fault.New(fault.Invalid, "Redis job backend is not initialized")
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return jobReply{}, err
	}
	raw, err := b.client.execute(ctx, func(ctx context.Context, c *driver.Client) (any, error) {
		return c.Eval(ctx, jobsScript, jobKeys(key), b.identity, b.limits, string(encoded)).Result()
	})
	if err != nil {
		return jobReply{}, err
	}
	parts, ok := raw.([]any)
	if !ok || len(parts) == 0 {
		return jobReply{}, fault.New(fault.Internal, "invalid Redis job reply")
	}
	status, ok := parts[0].(int64)
	if !ok {
		return jobReply{}, fault.New(fault.Internal, "invalid Redis job status")
	}
	switch status {
	case -2:
		return jobReply{}, fault.New(fault.Invalid, "Redis job state or operation is invalid")
	case -3:
		return jobReply{}, fault.New(fault.Conflict, "Redis job identity, capacity or queue policy conflicts")
	case -4:
		return jobReply{}, jobs.ErrOwnershipLost
	case -5:
		return jobReply{}, jobs.ErrCancelled
	case -6:
		return jobReply{}, jobs.ErrNotUnique
	case 1:
	default:
		return jobReply{}, fault.New(fault.Internal, "invalid Redis job status")
	}
	if len(parts) != 2 {
		return jobReply{}, fault.New(fault.Internal, "invalid Redis job reply size")
	}
	text, ok := parts[1].(string)
	if !ok {
		return jobReply{}, fault.New(fault.Internal, "invalid Redis job result")
	}
	var reply jobReply
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return jobReply{}, fault.New(fault.Internal, "invalid Redis job result encoding")
	}
	return reply, nil
}
func (b *JobBackend) JobEnqueue(ctx context.Context, key jobs.Key, envelope jobs.Envelope) (bool, error) {
	if err := jobs.ValidateEnqueue(ctx, key, envelope); err != nil {
		return false, err
	}
	data, err := envelope.MarshalJSON()
	if err != nil {
		return false, err
	}
	available := jobAvailableAt(envelope)
	reply, err := b.command(ctx, key, jobRequest{Op: "enqueue", Unique: envelope.Uniqueness().Digest, UniqueFor: envelope.Uniqueness().For.Milliseconds(), ID: envelope.ID().String(), Envelope: string(data), Name: envelope.Name(), Version: envelope.Version(), Maximum: envelope.Policy().Attempts, Available: available, Bytes: int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))})
	return reply.Inserted, err
}

// Redis eligibility uses milliseconds. Round a future boundary up so a
// sub-millisecond scheduled instant never becomes eligible before its timestamp.
func jobAvailableAt(envelope jobs.Envelope) int64 {
	at := envelope.AvailableAt()
	if at.IsZero() {
		return 0
	}
	result := at.UnixMilli()
	if at.Nanosecond()%int(time.Millisecond) != 0 {
		result++
	}
	return result
}
func (b *JobBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	if err := owner.Validate(); err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	if err := lease.ValidateDuration(ttl); err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	reply, err := b.command(ctx, key, jobRequest{Op: "reserve", Owner: hex.EncodeToString(owner.Bytes()), TTL: ceilMilliseconds(ttl)})
	if err != nil || reply.Record == nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	record, err := reply.Record.decode(key)
	if err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	proof, err := jobs.NewOwnership(record.Envelope.ID(), owner)
	if err != nil {
		return value.Optional[jobs.Reservation]{}, err
	}
	return value.Set(jobs.Reservation{Envelope: record.Envelope, Ownership: proof, Attempts: record.Attempts, ExpiresAt: record.LeaseExpiresAt}), nil
}
func proofRequest(op string, proof jobs.Ownership) jobRequest {
	return jobRequest{Op: op, ID: proof.ID().String(), Owner: hex.EncodeToString(proof.Owner().Bytes())}
}
func (b *JobBackend) JobStart(ctx context.Context, key jobs.Key, proof jobs.Ownership) (uint32, error) {
	if err := proof.Validate(); err != nil {
		return 0, err
	}
	reply, err := b.command(ctx, key, proofRequest("start", proof))
	return reply.Attempt, err
}
func (b *JobBackend) JobRenew(ctx context.Context, key jobs.Key, proof jobs.Ownership, ttl time.Duration) (jobs.LeaseStatus, error) {
	if err := proof.Validate(); err != nil {
		return jobs.LeaseStatus{}, err
	}
	if err := lease.ValidateDuration(ttl); err != nil {
		return jobs.LeaseStatus{}, err
	}
	request := proofRequest("renew", proof)
	request.TTL = ceilMilliseconds(ttl)
	reply, err := b.command(ctx, key, request)
	return jobs.LeaseStatus{Owned: reply.Owned, CancellationRequested: reply.Cancelled}, err
}
func (b *JobBackend) JobFinish(ctx context.Context, key jobs.Key, proof jobs.Ownership, result jobs.Result) (bool, error) {
	if err := proof.Validate(); err != nil {
		return false, err
	}
	if err := result.Validate(); err != nil {
		return false, err
	}
	request := proofRequest("finish", proof)
	request.State = result.State
	request.Reason = result.Reason
	if result.Delay > 0 {
		request.Delay = ceilMilliseconds(result.Delay)
	}
	reply, err := b.command(ctx, key, request)
	return reply.Changed, err
}
func (b *JobBackend) JobCancel(ctx context.Context, key jobs.Key, target jobs.Target) (bool, error) {
	if err := target.Validate(); err != nil {
		return false, err
	}
	reply, err := b.command(ctx, key, jobRequest{Op: "cancel", ID: target.ID.String(), Name: target.Name, Version: target.Version})
	return reply.Changed, err
}
func (b *JobBackend) JobInspect(ctx context.Context, key jobs.Key, id jobs.ExecutionID) (value.Optional[jobs.Record], error) {
	if id.IsZero() {
		return value.Optional[jobs.Record]{}, fault.New(fault.Invalid, "job inspection requires an identity")
	}
	reply, err := b.command(ctx, key, jobRequest{Op: "inspect", ID: id.String()})
	if err != nil || reply.Record == nil {
		return value.Optional[jobs.Record]{}, err
	}
	record, err := reply.Record.decode(key)
	if err != nil {
		return value.Optional[jobs.Record]{}, err
	}
	return value.Set(record), nil
}
func (r *jobStoredRecord) decode(key jobs.Key) (jobs.Record, error) {
	envelope, err := jobs.DecodeEnvelope([]byte(r.Envelope))
	if err != nil {
		return jobs.Record{}, err
	}
	if err := jobs.ValidateEnqueue(context.Background(), key, envelope); err != nil {
		return jobs.Record{}, err
	}
	if envelope.ID().String() != r.ID {
		return jobs.Record{}, fault.New(fault.Invalid, "stored Redis job identity differs")
	}
	result := jobs.Record{Workflow: r.Workflow, Position: r.Position, Envelope: envelope, State: r.State, Attempts: r.Attempts, AvailableAt: time.UnixMilli(r.Available).UTC(), CreatedAt: time.UnixMilli(r.Created).UTC(), CancellationRequested: r.Cancelled}
	if r.Expiry != 0 {
		result.LeaseExpiresAt = time.UnixMilli(r.Expiry).UTC()
	}
	if r.Finished != 0 {
		result.FinishedAt = time.UnixMilli(r.Finished).UTC()
	}
	for _, item := range r.History {
		result.History = append(result.History, jobs.Transition{State: item.State, At: time.UnixMilli(item.At).UTC(), Attempt: item.Attempt, Reason: item.Reason})
	}
	return result, nil
}

func (b *JobBackend) JobWorkflow(ctx context.Context, key jobs.Key, workflow jobs.WorkflowEnvelope) (bool, error) {
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
	digest := sha256.Sum256(data)
	request := jobRequest{Op: "workflow", ID: workflow.ID().String(), WorkflowKind: workflow.Kind(), Fingerprint: hex.EncodeToString(digest[:])}
	members := workflow.Steps()
	if completion, ok := workflow.Completion().Get(); ok {
		members = append(members, completion)
		request.Completion = completion.ID().String()
	}
	request.GroupBytes = int64(256 + len(members)*32)
	for _, envelope := range members {
		data, err := envelope.MarshalJSON()
		if err != nil {
			return false, err
		}
		available := jobAvailableAt(envelope)
		request.Steps = append(request.Steps, jobRequest{ID: envelope.ID().String(), Envelope: string(data), Name: envelope.Name(), Version: envelope.Version(), Maximum: envelope.Policy().Attempts, Available: available, Bytes: int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))})
	}
	reply, err := b.command(ctx, key, request)
	return reply.Inserted, err
}
func (b *JobBackend) JobCancelWorkflow(ctx context.Context, key jobs.Key, id jobs.WorkflowID) (bool, error) {
	if id.IsZero() {
		return false, fault.New(fault.Invalid, "workflow cancellation requires an identity")
	}
	reply, err := b.command(ctx, key, jobRequest{Op: "cancel_workflow", ID: id.String()})
	return reply.Changed, err
}

func (b *JobBackend) JobList(ctx context.Context, key jobs.Key, options jobs.ListOptions) (jobs.Page, error) {
	if err := options.Validate(); err != nil {
		return jobs.Page{}, err
	}
	request := jobRequest{Op: "list", Limit: options.Limit, Name: options.Name, Version: options.Version, State: options.State}
	if !options.After.IsZero() {
		request.After = options.After.String()
	}
	reply, err := b.command(ctx, key, request)
	if err != nil {
		return jobs.Page{}, err
	}
	page := jobs.Page{}
	for _, item := range reply.Records {
		record, err := item.decode(key)
		if err != nil {
			return jobs.Page{}, err
		}
		page.Records = append(page.Records, record)
	}
	if reply.Next != "" {
		id, err := model.ParseID[jobs.Execution](reply.Next)
		if err != nil {
			return jobs.Page{}, err
		}
		page.Next = id
	}
	return page, nil
}

// DurableAcceptance describes Redis acknowledgement under the deployment's
// persistence/failover policy; it is not an fsync or replication guarantee.
func (*JobBackend) DurableAcceptance() bool { return true }
