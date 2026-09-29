package redis

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"sync"
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
// terminal retention expires or retained-record eviction; durability depends on
// Redis persistence/failover. Stop workers and publication before closing the
// client's owning module.
//
// Queues use storage layout 2: the immutable envelope is stored apart from
// small mutable state. A layout-1 queue is never migrated implicitly: every
// operation returns jobs.ErrLegacyLayout until an operator stops or drains the
// previous release and runs JobMigrateLayout (`jobs migrate-layout`). The
// previous release cannot read a migrated queue.
type JobBackend struct {
	client   *Client
	config   jobs.QueueConfig
	identity string
	legacy   string
	limits   string
	mu       sync.Mutex
	wake     map[jobs.Key]chan struct{}
}

// legacyQueueConfig reproduces the layout-1 policy identity for migration.
type legacyQueueConfig struct {
	MaxEntries int
	MaxBytes   int64
	MaxHistory int
	Retention  time.Duration
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
	legacy, err := json.Marshal(legacyQueueConfig{MaxEntries: config.MaxEntries, MaxBytes: config.MaxBytes, MaxHistory: config.MaxHistory, Retention: config.Retention})
	if err != nil {
		return nil, err
	}
	limits, err := json.Marshal(map[string]any{
		"entries": config.MaxEntries, "bytes": config.MaxBytes, "history": config.MaxHistory,
		"retained": config.RetainedLimit(), "delivery_floor": 3,
		"retention": config.Retention.Milliseconds(), "max_delay": jobs.MaxDelay.Milliseconds(),
		"record_bytes": 2*(jobs.MaxPayloadBytes+64*1024) + config.MaxHistory*256,
		"owner_bytes":  lease.OwnerBytes * 2, "attempts": jobs.MaxAttempts, "scan_limit": jobs.ListScanLimit,
		"workflow_steps": jobs.MaxWorkflowSteps, "workflow_members": jobs.MaxWorkflowMembers,
		"manual_retries": jobs.MaxManualRetries,
	})
	if err != nil {
		return nil, err
	}
	return &JobBackend{client: client, config: config, identity: string(identity), legacy: string(legacy), limits: string(limits)}, nil
}

var _ jobs.Backend = (*JobBackend)(nil)
var _ jobs.WakeBackend = (*JobBackend)(nil)
var _ jobs.StatsBackend = (*JobBackend)(nil)
var _ jobs.ForgetBackend = (*JobBackend)(nil)

// JobWakeup wakes idle workers of this process subscribed to key after this
// process's own accepted enqueue in that queue. Workers in other processes rely
// on their adaptive idle polling.
func (b *JobBackend) JobWakeup(key jobs.Key) <-chan struct{} {
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
func (b *JobBackend) signal(key jobs.Key) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if wake, ok := b.wake[key]; ok {
		close(wake)
		delete(b.wake, key)
	}
}

type jobRequest struct {
	RetryToken       jobs.RetryToken   `json:"retry_token,omitempty"`
	ExpectedCreated  int64             `json:"expected_created"`
	ExpectedFinished int64             `json:"expected_finished"`
	ExpectedAttempts uint32            `json:"expected_attempts"`
	ExpectedRetries  uint32            `json:"expected_retries"`
	MaxRetries       uint32            `json:"max_retries,omitempty"`
	After            string            `json:"after"`
	Limit            int               `json:"limit"`
	WorkflowKind     jobs.WorkflowKind `json:"workflow_kind,omitempty"`
	Steps            []jobRequest      `json:"steps,omitempty"`
	Completion       string            `json:"completion"`
	Catch            string            `json:"catch,omitempty"`
	Finally          string            `json:"finally,omitempty"`
	Fingerprint      string            `json:"fingerprint,omitempty"`
	GroupBytes       int64             `json:"group_bytes,omitempty"`
	Op               string            `json:"op"`
	Unique           string            `json:"unique"`
	UniqueFor        int64             `json:"unique_for"`
	ID               string            `json:"id,omitempty"`
	Owner            string            `json:"owner,omitempty"`
	TTL              int64             `json:"ttl,omitempty"`
	Envelope         string            `json:"envelope,omitempty"`
	Name             jobs.Name         `json:"name,omitempty"`
	Version          jobs.Version      `json:"version,omitempty"`
	Maximum          uint32            `json:"maximum,omitempty"`
	Available        int64             `json:"available"`
	Bytes            int64             `json:"bytes,omitempty"`
	State            jobs.State        `json:"state,omitempty"`
	Reason           jobs.Reason       `json:"reason"`
	Delay            int64             `json:"delay"`
	Refund           bool              `json:"refund,omitempty"`
	UniqueRelease    bool              `json:"unique_release,omitempty"`
}
type jobStoredRecord struct {
	Retries    uint32          `json:"retries"`
	LastRetry  jobs.RetryToken `json:"last_retry"`
	Workflow   jobs.WorkflowID `json:"workflow,omitempty"`
	Position   uint32          `json:"position"`
	ID         string          `json:"id"`
	Envelope   string          `json:"envelope"`
	State      jobs.State      `json:"state"`
	Attempts   uint32          `json:"attempts"`
	Exceptions uint32          `json:"exceptions"`
	Available  int64           `json:"available"`
	Expiry     int64           `json:"expiry"`
	Created    int64           `json:"created"`
	Finished   int64           `json:"finished"`
	Cancelled  bool            `json:"cancelled"`
	History    []struct {
		Retry   uint32      `json:"retry"`
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
	Stats     *jobs.QueueStats  `json:"stats"`
	Workflow  *workflowStatus   `json:"workflow"`
}

// workflowStatus is the script's status reply; finished is Unix milliseconds.
type workflowStatus struct {
	ID         string            `json:"id"`
	Kind       jobs.WorkflowKind `json:"kind"`
	Total      int               `json:"total"`
	Pending    int               `json:"pending"`
	Processed  int               `json:"processed"`
	Succeeded  int               `json:"succeeded"`
	FailedJobs int               `json:"failed_jobs"`
	Cancelled  int               `json:"cancelled"`
	Failed     bool              `json:"failed"`
	Cancelling bool              `json:"cancelling"`
	Finished   int64             `json:"finished"`
}

func jobKeys(key jobs.Key) []string {
	base := key.String()
	return []string{base + ":records", base + ":ready", base + ":leases", base + ":finished", base + ":metadata", base + ":unique", base + ":workflows", base + ":workflow_finished", base + ":index", base + ":envelopes"}
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
		return evalScript(ctx, c, jobsScript, jobKeys(key), b.identity, b.limits, string(encoded), b.legacy).Result()
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
		return jobReply{}, fault.New(fault.Conflict, "Redis job identity already belongs to a different envelope")
	case -4:
		return jobReply{}, jobs.ErrOwnershipLost
	case -5:
		return jobReply{}, jobs.ErrCancelled
	case -6:
		return jobReply{}, jobs.ErrNotUnique
	case -7:
		return jobReply{}, jobs.ErrNotRetryable
	case -8:
		return jobReply{}, jobs.ErrQueueFull
	case -9:
		return jobReply{}, jobs.ErrQueuePolicy
	case -10:
		return jobReply{}, jobs.ErrLegacyLayout
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
	reply, err := b.command(ctx, key, jobRequest{Op: "enqueue", Unique: envelope.Uniqueness().Digest, UniqueFor: envelope.Uniqueness().For.Milliseconds(), UniqueRelease: envelope.Uniqueness().UntilProcessing, ID: envelope.ID().String(), Envelope: string(data), Name: envelope.Name(), Version: envelope.Version(), Maximum: envelope.Policy().Attempts, Available: available, Bytes: int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))})
	if err == nil && reply.Inserted {
		b.signal(key)
	}
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
	return value.Set(jobs.Reservation{Retries: record.Retries, Envelope: record.Envelope, Ownership: proof, Attempts: record.Attempts, Exceptions: record.Exceptions, ExpiresAt: record.LeaseExpiresAt}), nil
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
	request.Refund = result.Refund
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
	result := jobs.Record{Retries: r.Retries, LastRetry: r.LastRetry, Workflow: r.Workflow, Position: r.Position, Envelope: envelope, State: r.State, Attempts: r.Attempts, Exceptions: r.Exceptions, AvailableAt: time.UnixMilli(r.Available).UTC(), CreatedAt: time.UnixMilli(r.Created).UTC(), CancellationRequested: r.Cancelled}
	if r.Expiry != 0 {
		result.LeaseExpiresAt = time.UnixMilli(r.Expiry).UTC()
	}
	if r.Finished != 0 {
		result.FinishedAt = time.UnixMilli(r.Finished).UTC()
	}
	for _, item := range r.History {
		result.History = append(result.History, jobs.Transition{State: item.State, At: time.UnixMilli(item.At).UTC(), Attempt: item.Attempt, Reason: item.Reason, Retry: item.Retry})
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
	members := workflow.Members()
	if completion, ok := workflow.Completion().Get(); ok {
		request.Completion = completion.ID().String()
	}
	if catch, ok := workflow.Catch().Get(); ok {
		request.Catch = catch.ID().String()
	}
	if finally, ok := workflow.Finally().Get(); ok {
		request.Finally = finally.ID().String()
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
	if err == nil && reply.Inserted {
		b.signal(key)
	}
	return reply.Inserted, err
}

// JobWorkflowStatus reports one workflow's progress without payloads.
func (b *JobBackend) JobWorkflowStatus(ctx context.Context, key jobs.Key, id jobs.WorkflowID) (value.Optional[jobs.WorkflowStatus], error) {
	if id.IsZero() {
		return value.Optional[jobs.WorkflowStatus]{}, fault.New(fault.Invalid, "workflow status requires an identity")
	}
	reply, err := b.command(ctx, key, jobRequest{Op: "workflow_status", ID: id.String()})
	if err != nil || reply.Workflow == nil {
		return value.Optional[jobs.WorkflowStatus]{}, err
	}
	w := reply.Workflow
	if w.ID != id.String() || w.Total != w.Pending+w.Processed || w.Processed != w.Succeeded+w.FailedJobs+w.Cancelled {
		return value.Optional[jobs.WorkflowStatus]{}, fault.New(fault.Invalid, "invalid Redis workflow status")
	}
	status := jobs.WorkflowStatus{ID: id, Kind: w.Kind, Total: w.Total, Pending: w.Pending, Processed: w.Processed, Succeeded: w.Succeeded, FailedJobs: w.FailedJobs, Cancelled: w.Cancelled, Failed: w.Failed, Cancelling: w.Cancelling}
	if w.Finished > 0 {
		status.FinishedAt = time.UnixMilli(w.Finished).UTC()
	}
	return value.Set(status), nil
}

var _ jobs.WorkflowStatusBackend = (*JobBackend)(nil)
var _ jobs.LayoutMigrator = (*JobBackend)(nil)

// JobMigrateLayout moves a layout-1 queue to layout 2 in one atomic script.
// Records keep their IDs, state and history; embedded envelopes move on each
// record's next write. It is idempotent.
func (b *JobBackend) JobMigrateLayout(ctx context.Context, key jobs.Key) (bool, error) {
	reply, err := b.command(ctx, key, jobRequest{Op: "migrate_layout"})
	return reply.Changed, err
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

// JobStats reports queue depth from indexes and counters, without payloads.
func (b *JobBackend) JobStats(ctx context.Context, key jobs.Key) (jobs.QueueStats, error) {
	reply, err := b.command(ctx, key, jobRequest{Op: "stats"})
	if err != nil {
		return jobs.QueueStats{}, err
	}
	if reply.Stats == nil {
		return jobs.QueueStats{}, fault.New(fault.Internal, "invalid Redis job statistics")
	}
	return *reply.Stats, nil
}

// JobForget removes one retained terminal independent record, its envelope and
// its deduplication identity. Live and workflow records are never removed.
func (b *JobBackend) JobForget(ctx context.Context, key jobs.Key, target jobs.Target) (bool, error) {
	if err := target.Validate(); err != nil {
		return false, err
	}
	reply, err := b.command(ctx, key, jobRequest{Op: "forget", ID: target.ID.String(), Name: target.Name, Version: target.Version})
	return reply.Changed, err
}
