package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MaxManualRetries bounds operator-triggered execution cycles of one retained job.
const MaxManualRetries = 1000

// RetryToken identifies one observed failed state. It is a concurrency token,
// not authorization. Save it before retrying; reuse it after an ambiguous result.
type RetryToken string

func (t RetryToken) Validate() error {
	if len(t) != sha256.Size*2 || strings.ToLower(string(t)) != string(t) {
		return fault.New(fault.Invalid, "invalid job retry token")
	}
	if _, err := hex.DecodeString(string(t)); err != nil {
		return fault.New(fault.Invalid, "invalid job retry token")
	}
	return nil
}

var ErrNotRetryable = fault.New(fault.Conflict, "job is not retryable in the observed failed state")

// RetryToken returns a token for an independent failed job. Workflow members must
// not be reopened independently: their dependent transitions are already committed.
func (r Record) RetryToken() (RetryToken, error) {
	if r.State != Failed || !r.Workflow.IsZero() || r.CancellationRequested || r.Retries >= MaxManualRetries || r.CreatedAt.IsZero() || r.FinishedAt.IsZero() {
		return "", ErrNotRetryable
	}
	data, err := r.Envelope.MarshalJSON()
	if err != nil {
		return "", err
	}
	// Keep the fingerprint at one owner; adapters compare the observed record
	// atomically before applying a retry. No payload is emitted with the token.
	source, err := json.Marshal(struct {
		Envelope          string
		Created, Finished string
		Attempts, Retries uint32
	}{string(data), r.CreatedAt.UTC().Format(time.RFC3339Nano), r.FinishedAt.UTC().Format(time.RFC3339Nano), r.Attempts, r.Retries})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(source)
	return RetryToken(hex.EncodeToString(digest[:])), nil
}

// RetryRequest is the explicit heterogeneous adapter/operations boundary.
type RetryRequest struct {
	Target Target
	Token  RetryToken
}

func (r RetryRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	return r.Token.Validate()
}

// CheckRetry checks one owned snapshot. Backends must keep the snapshot current
// through their atomic mutation. True means the same retry was already applied,
// even if its new execution has since finished. Older tokens never reopen it.
func CheckRetry(record Record, request RetryRequest) (bool, error) {
	if err := request.Validate(); err != nil {
		return false, err
	}
	if record.Envelope.Target() != request.Target {
		return false, ErrNotRetryable
	}
	if record.LastRetry == request.Token {
		return true, nil
	}
	token, err := record.RetryToken()
	if err != nil {
		return false, err
	}
	if token != request.Token {
		return false, ErrNotRetryable
	}
	return false, nil
}

// RetryBackend is optional so existing custom Backend implementations remain
// source-compatible. Built-in memory and Redis authorities implement it.
// A successful mutation keeps the envelope/ID/history, resets attempt budget,
// increments Retries and makes work immediately eligible. Only a matching failed
// independent job may change. Network errors do not establish acceptance.
type RetryBackend interface {
	JobRetry(context.Context, Key, RetryRequest) (bool, error)
}

// Retry is an explicit operator action, never an automatic error recovery loop.
// changed=false with nil error confirms this token was already applied.
func (d *Dispatcher) Retry(ctx context.Context, queue Queue, request RetryRequest) (bool, error) {
	changed, err := d.retry(ctx, queue, request)
	if err == nil && changed {
		if key, keyErr := NewKey(d.config.Namespace, queue); keyErr == nil {
			d.runInline(ctx, key)
		}
	}
	return changed, err
}
func (d *Dispatcher) retry(ctx context.Context, queue Queue, request RetryRequest) (bool, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	if err := request.Validate(); err != nil {
		return false, err
	}
	if _, err := d.registry.lookup(jobKey{request.Target.Name, request.Target.Version}); err != nil {
		return false, err
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return false, err
	}
	backend, ok := d.backend.(RetryBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "job backend does not support manual retry")
	}
	return backend.JobRetry(ctx, key, request)
}

// Retry preserves the concrete payload owner. Obtain token from Inspect's failed
// Record and retain it when reconciling an uncertain backend response.
func (d Definition[P]) Retry(ctx context.Context, dispatcher *Dispatcher, id ID[P], queue Queue, token RetryToken) (bool, error) {
	if dispatcher == nil || dispatcher.registry == nil {
		return false, fault.New(fault.Invalid, "job retry requires a dispatcher")
	}
	if err := d.check(dispatcher.registry); err != nil {
		return false, err
	}
	if queue == "" {
		queue = d.policy.Queue
	}
	return dispatcher.Retry(ctx, queue, RetryRequest{Target: Target{ID: model.IDFromBytes[Execution](id.Bytes()), Name: d.name, Version: d.version}, Token: token})
}

func (b Bound[P]) Retry(ctx context.Context, id ID[P], queue Queue, token RetryToken) (bool, error) {
	options, err := b.options(Options[P]{Queue: queue})
	if err != nil {
		return false, err
	}
	return b.definition.Retry(ctx, b.connection.dispatcher, id, options.Queue, token)
}
