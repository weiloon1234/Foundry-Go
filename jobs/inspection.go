package jobs

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxListLimit bounds the number of records returned by one inspection page.
const MaxListLimit = 100

// ListScanLimit bounds the number of identities examined for one inspection page.
const ListScanLimit = 128

// ListOptions scans execution IDs in ascending order. Zero After begins a scan;
// zero Name/Version and State select all records. Pagination is weakly consistent
// under concurrent enqueue/expiry; Next may be nonzero for an empty filtered page.
type ListOptions struct {
	After   ExecutionID
	Name    Name
	Version Version
	State   State
	Limit   int
}

// Validate checks the page limit, paired schema filter, and recognized state.
func (o ListOptions) Validate() error {
	if err := validateListLimit(o.Limit); err != nil {
		return err
	}
	if (o.Name == "") != (o.Version == 0) || (o.Name != "" && !identifier.Semantic(string(o.Name))) {
		return safeerror.NewFault(fault.Invalid, "job list schema filter requires name and version")
	}
	switch o.State {
	case "", Waiting, Blocked, Reserved, Running, Succeeded, Failed, Cancelled:
		return nil
	}
	return safeerror.NewFault(fault.Invalid, "invalid job list state")
}

func validateListLimit(limit int) error {
	if limit < 1 || limit > MaxListLimit {
		return safeerror.NewFault(fault.Invalid, "job list limit must be between 1 and 100")
	}
	return nil
}

// QuarantineOptions scans quarantined execution IDs in ascending order.
// Zero After begins a scan. An empty page can still have a continuation cursor.
type QuarantineOptions struct {
	After ExecutionID
	Limit int
}

// Validate checks the shared inspection page limit.
func (o QuarantineOptions) Validate() error { return validateListLimit(o.Limit) }

// QuarantinedRecord identifies independent corrupt work without exposing its data.
// QuarantinedAt is the first observation time, not the job's completion time.
type QuarantinedRecord struct {
	ID            ExecutionID `json:"id"`
	QuarantinedAt time.Time   `json:"quarantined_at"`
}

// QuarantinePage contains safe metadata and a continuation identity, which may
// be nonzero even when Records is empty. Pagination is weakly consistent.
type QuarantinePage struct {
	Records []QuarantinedRecord `json:"records"`
	Next    ExecutionID         `json:"next,omitzero"`
}

// QuarantineBackend is an optional inspection capability. Quarantine preserves
// raw data, deduplication identities and accounting; it does not authorize deletion.
type QuarantineBackend interface {
	// JobQuarantined lists existing quarantine metadata without decoding damaged records.
	JobQuarantined(context.Context, Key, QuarantineOptions) (QuarantinePage, error)
}

// Quarantined lists safe metadata through the optional QuarantineBackend.
func (d *Dispatcher) Quarantined(ctx context.Context, queue Queue, options QuarantineOptions) (QuarantinePage, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return QuarantinePage{}, err
	}
	defer release()
	if err := options.Validate(); err != nil {
		return QuarantinePage{}, err
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return QuarantinePage{}, err
	}
	backend, ok := d.backend.(QuarantineBackend)
	if !ok {
		return QuarantinePage{}, safeerror.NewFault(fault.Invalid, "job backend does not report quarantine metadata")
	}
	return backend.JobQuarantined(ctx, key, options)
}

// Matches reports whether the record satisfies the state and schema filters.
func (o ListOptions) Matches(r Record) bool {
	return (o.State == "" || o.State == r.State) && (o.Name == "" || (o.Name == r.Envelope.Name() && o.Version == r.Envelope.Version()))
}

// Page contains owned record snapshots and the cursor for a further bounded scan.
type Page struct {
	Records []Record
	Next    ExecutionID
}

// Summary is safe operational metadata: no payload, origin, lease owner or raw
// error. RetryToken identifies the current failed state; it grants no permission.
type Summary struct {
	ID          ExecutionID `json:"id"`
	Name        Name        `json:"name"`
	Version     Version     `json:"version"`
	Queue       Queue       `json:"queue"`
	State       State       `json:"state"`
	Attempts    uint32      `json:"attempts"`
	MaxAttempts uint32      `json:"max_attempts"`
	Retries     uint32      `json:"retries"`
	Exceptions  uint32      `json:"exceptions,omitzero"`
	Reason      Reason      `json:"reason,omitempty"`
	Workflow    WorkflowID  `json:"workflow,omitzero"`
	AvailableAt time.Time   `json:"available_at"`
	CreatedAt   time.Time   `json:"created_at"`
	FinishedAt  time.Time   `json:"finished_at,omitzero"`
	RetryToken  RetryToken  `json:"retry_token,omitempty"`
}

// Summary returns operational metadata without payloads or origin, including a retry token when eligible.
func (r Record) Summary() Summary {
	token, _ := r.RetryToken()
	result := Summary{ID: r.Envelope.ID(), Name: r.Envelope.Name(), Version: r.Envelope.Version(), Queue: r.Envelope.Queue(), State: r.State, Attempts: r.Attempts, MaxAttempts: r.Envelope.Policy().Attempts, Retries: r.Retries, Exceptions: r.Exceptions, Workflow: r.Workflow, AvailableAt: r.AvailableAt, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt, RetryToken: token}
	if len(r.History) > 0 {
		result.Reason = r.History[len(r.History)-1].Reason
	}
	return result
}

// Inspect is the explicit heterogeneous operations boundary. Prefer the typed
// Definition.Inspect in application domain code. Records contain private payloads;
// use Summary when emitting operator output.
func (d *Dispatcher) Inspect(ctx context.Context, queue Queue, id ExecutionID) (value.Optional[Record], error) {
	release, err := d.begin(ctx)
	if err != nil {
		return value.Optional[Record]{}, err
	}
	defer release()
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return value.Optional[Record]{}, err
	}
	if id.IsZero() {
		return value.Optional[Record]{}, safeerror.NewFault(fault.Invalid, "job inspection requires an identity")
	}
	return d.backend.JobInspect(ctx, key, id)
}

// List is the explicit operational boundary. It returns owned payload/history
// snapshots; callers must authorize inspection before exposing them over HTTP.
func (d *Dispatcher) List(ctx context.Context, queue Queue, options ListOptions) (Page, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return Page{}, err
	}
	defer release()
	if err := options.Validate(); err != nil {
		return Page{}, err
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return Page{}, err
	}
	return d.backend.JobList(ctx, key, options)
}
