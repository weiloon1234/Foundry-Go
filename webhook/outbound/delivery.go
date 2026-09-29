package outbound

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookstore"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DeliveryState is the delivery log state of one message.
type DeliveryState string

const (
	Pending   DeliveryState = "pending"
	Succeeded DeliveryState = "succeeded"
	Failed    DeliveryState = "failed"
)

// DeliveryInfo is one delivery log entry. It never contains the payload,
// response body, secrets or transport error text; LastFailure is a stable
// category and LastStatus the last HTTP status (0 when none was received).
type DeliveryInfo struct {
	ID          DeliveryID
	Endpoint    EndpointID
	Event       EventType
	State       DeliveryState
	Attempts    uint32
	LastStatus  int
	LastFailure string
	CreatedAt   temporal.DateTime
	DeliveredAt value.Nullable[temporal.DateTime]
}

func deliveryInfo(row store.Delivery) DeliveryInfo {
	return DeliveryInfo{ID: model.IDFromBytes[Delivery](row.ID.Bytes()), Endpoint: model.IDFromBytes[Endpoint](row.EndpointID.Bytes()), Event: EventType(row.Event), State: DeliveryState(row.State), Attempts: row.Attempts, LastStatus: int(row.LastStatus), LastFailure: row.LastFailure, CreatedAt: row.CreatedAt, DeliveredAt: row.DeliveredAt}
}
func deliveryKey(id DeliveryID) model.ID[store.Delivery] {
	return model.IDFromBytes[store.Delivery](id.Bytes())
}

// maxResponseDrain bounds how much of a receiver's response body is read.
const maxResponseDrain = 16 << 10

// WebhookID is the stable Standard Webhooks message ID of a delivery. It is
// identical across retries and replays, so receivers deduplicate by it.
func WebhookID(id DeliveryID) string { return "msg_" + id.String() }

// DeliveryRequest is the typed job payload: only the durable delivery ID.
type DeliveryRequest struct {
	Delivery DeliveryID `json:"delivery"`
}

func (DeliveryRequest) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("webhook delivery request"))
}

// DeliveryJob sends one logged delivery. Retries follow the job policy's
// attempts and backoff; the final attempt records the delivery as failed.
type DeliveryJob struct {
	definition jobs.Definition[DeliveryRequest]
}

func DefineDeliveryJob(name jobs.Name, policy jobs.Policy) DeliveryJob {
	return DeliveryJob{definition: jobs.Define[DeliveryRequest](name, 1, policy)}
}

// Definition returns the typed job definition, for registry inspection and
// explicit dispatch by operational tooling.
func (j DeliveryJob) Definition() jobs.Definition[DeliveryRequest] { return j.definition }
func (j DeliveryJob) Declare(service *Service) (jobs.Declaration, error) {
	if err := service.Validate(); err != nil {
		return jobs.Declaration{}, err
	}
	return j.definition.Declare(func(ctx context.Context, request DeliveryRequest) error {
		if request.Delivery.IsZero() {
			return jobs.Permanent(invalid())
		}
		return service.Deliver(ctx, request.Delivery)
	})
}

// FailureSink records deliveries whose job the runtime finalized as failed
// (attempts, retry deadline or exception limit exhausted, timeout, lost
// lease) as failed. Register it for every worker that runs this job
// (jobs.WithFailureSink or jobs.RegisterFailureSink); otherwise such a
// delivery keeps its last failure category but stays pending. Sinks run at
// most once per terminal failure.
func (j DeliveryJob) FailureSink(service *Service) (jobs.FailureSink, error) {
	if err := service.Validate(); err != nil {
		return nil, err
	}
	if err := j.definition.Validate(); err != nil {
		return nil, err
	}
	return deliverySink{job: j, service: service}, nil
}

type deliverySink struct {
	job     DeliveryJob
	service *Service
}

func (d deliverySink) RecordFailure(ctx context.Context, failed jobs.FailedJob) error {
	if failed.Envelope.Name() != d.job.definition.Name() || failed.Envelope.Version() != d.job.definition.Version() {
		return nil
	}
	request, err := d.job.definition.Payload(ctx, failed.Envelope)
	if err != nil {
		return err
	}
	if request.Delivery.IsZero() {
		return nil
	}
	return d.service.finalize(ctx, deliveryKey(request.Delivery), "job_"+string(failed.Reason))
}

// Queue binds a service to its delivery job and outbox producer. Construct the
// service, declare its job, assemble the dispatcher and outbox, then bind.
type Queue struct {
	service  *Service
	job      DeliveryJob
	producer *jobs.Outbox
}

func (j DeliveryJob) ToOutbox(service *Service, producer *jobs.Outbox) (Queue, error) {
	q := Queue{service: service, job: j, producer: producer}
	if err := q.Validate(); err != nil {
		return Queue{}, err
	}
	return q, nil
}
func (q Queue) Validate() error {
	if err := q.service.Validate(); err != nil {
		return err
	}
	if err := q.job.definition.Validate(); err != nil {
		return err
	}
	if q.producer == nil {
		return invalid()
	}
	return q.producer.Destination().Validate()
}
func (Queue) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbound webhook queue")) }

// Service returns the bound service, for example to inspect deliveries.
func (q Queue) Service() *Service { return q.service }

func (q Queue) enqueue(ctx context.Context, tx *database.Tx, id model.ID[store.Delivery]) error {
	_, err := q.job.definition.Enqueue(ctx, tx, q.producer, DeliveryRequest{Delivery: model.IDFromBytes[Delivery](id.Bytes())}, jobs.Options[DeliveryRequest]{})
	return err
}

// envelope is the Standard Webhooks payload shape: type, timestamp and data.
func envelope[P any](ctx context.Context, service *Service, event Event[P], payload P, at time.Time) (string, error) {
	data, err := event.contract.Encode(ctx, payload, contract.JSONLimits{Bytes: int(service.config.MaxPayloadBytes), Depth: 32, Nodes: 10000, Steps: 100000, Issues: 16})
	if err != nil {
		return "", err
	}
	kind, err := json.Marshal(string(event.kind))
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	body.WriteString(`{"type":`)
	body.Write(kind)
	body.WriteString(`,"timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `","data":`)
	body.Write(data)
	body.WriteString(`}`)
	if int64(body.Len()) > service.config.MaxPayloadBytes {
		return "", fault.New(fault.Invalid, "webhook payload exceeds its limit")
	}
	return body.String(), nil
}

// Send logs one delivery of event to an active endpoint and enqueues its job
// in the caller's transaction: both commit or roll back with the business
// change. It performs no network I/O.
func Send[P any](ctx context.Context, tx *database.Tx, queue Queue, endpoint EndpointID, event Event[P], payload P) (DeliveryID, error) {
	if err := queue.Validate(); err != nil {
		return DeliveryID{}, err
	}
	if err := event.Validate(); err != nil {
		return DeliveryID{}, err
	}
	if endpoint.IsZero() || tx == nil {
		return DeliveryID{}, invalid()
	}
	var id model.ID[store.Delivery]
	err := queue.service.calls.Run(ctx, "webhook send", func(ctx context.Context) error {
		now, err := queue.service.now()
		if err != nil {
			return err
		}
		body, err := envelope(ctx, queue.service, event, payload, now.UTC())
		if err != nil {
			return err
		}
		return queue.service.join(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
			row, err := store.QueryFoundryWebhookEndpoints().RequireFind(ctx, tx, endpointKey(endpoint))
			if err != nil {
				return err
			}
			if !row.Active {
				return fault.New(fault.Conflict, "webhook endpoint is inactive")
			}
			id, err = queue.record(ctx, tx, row.ID, event.kind, body, now)
			return err
		})
	})
	if err != nil {
		return DeliveryID{}, err
	}
	return model.IDFromBytes[Delivery](id.Bytes()), nil
}

// Publish fans event out to every active endpoint subscribed to its type, in
// the caller's transaction. The payload is encoded once and each endpoint gets
// its own delivery ID.
func Publish[P any](ctx context.Context, tx *database.Tx, queue Queue, event Event[P], payload P) ([]DeliveryID, error) {
	if err := queue.Validate(); err != nil {
		return nil, err
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, invalid()
	}
	var ids []DeliveryID
	err := queue.service.calls.Run(ctx, "webhook publish", func(ctx context.Context) error {
		now, err := queue.service.now()
		if err != nil {
			return err
		}
		body, err := envelope(ctx, queue.service, event, payload, now.UTC())
		if err != nil {
			return err
		}
		return queue.service.join(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
			f := store.EndpointFields()
			rows, err := store.QueryFoundryWebhookEndpoints().Where(f.Active.Eq(true)).OrderBy(f.ID.Asc()).Limit(queue.service.config.MaxEndpoints+1).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) > queue.service.config.MaxEndpoints {
				return fault.New(fault.Conflict, "webhook fan-out exceeds its endpoint limit")
			}
			for _, row := range rows {
				events, err := row.Events.Decode()
				if err != nil {
					return err
				}
				if !subscribed(events, event.kind) {
					continue
				}
				id, err := queue.record(ctx, tx, row.ID, event.kind, body, now)
				if err != nil {
					return err
				}
				ids = append(ids, model.IDFromBytes[Delivery](id.Bytes()))
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}
func subscribed(events []string, kind EventType) bool {
	for _, event := range events {
		if event == string(kind) {
			return true
		}
	}
	return false
}
func (q Queue) record(ctx context.Context, tx *database.Tx, endpoint model.ID[store.Endpoint], kind EventType, body string, now temporal.DateTime) (model.ID[store.Delivery], error) {
	id, err := model.NewID[store.Delivery]()
	if err != nil {
		return id, err
	}
	if _, err := store.QueryFoundryWebhookDeliveries().Create(ctx, tx, store.DeliveryDraft{}.SetID(id).SetEndpointID(endpoint).SetEvent(string(kind)).SetPayload(body).SetState(string(Pending)).SetAttempts(0).SetLastStatus(0).SetLastFailure("").SetCreatedAt(now).SetUpdatedAt(now)); err != nil {
		return id, err
	}
	return id, q.enqueue(ctx, tx, id)
}

// sign produces the Standard Webhooks signature header: one "v1,<base64>"
// HMAC-SHA256 of "id.timestamp.body" per signing secret.
func sign(keys [][]byte, id, timestamp, body string) string {
	signatures := make([]string, len(keys))
	for i, key := range keys {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(id + "." + timestamp + "."))
		_, _ = mac.Write([]byte(body))
		signatures[i] = "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	return strings.Join(signatures, " ")
}

// Deliver sends one pending delivery now and records the attempt. A 2xx
// response succeeds (the response body is drained up to a small bound and
// never stored). Transport failures, timeouts, 408, 429 and 5xx, and failures
// before sending (storage, signing secrets), are retryable: the attempt is
// logged with its category and the delivery stays pending until the job
// runtime finalizes it (see DeliveryJob.FailureSink). Any other status, a
// denied destination or an inactive endpoint records the delivery as failed.
// Succeeded or failed deliveries are not resent; use Replay. The job handler
// calls it; it is exported for tools and tests.
func (s *Service) Deliver(ctx context.Context, id DeliveryID) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if id.IsZero() {
		return invalid()
	}
	return s.calls.Run(ctx, "webhook delivery", func(ctx context.Context) error {
		var delivery store.Delivery
		var endpoint store.Endpoint
		var keys [][]byte
		category := "store_failed"
		err := s.transaction(ctx, true, func(ctx context.Context, tx *database.Tx) error {
			var err error
			if delivery, err = store.QueryFoundryWebhookDeliveries().RequireFind(ctx, tx, deliveryKey(id)); err != nil {
				return err
			}
			if delivery.State != string(Pending) {
				return nil
			}
			if endpoint, err = store.QueryFoundryWebhookEndpoints().RequireFind(ctx, tx, delivery.EndpointID); err != nil {
				return err
			}
			if !endpoint.Active {
				return nil
			}
			now, err := s.now()
			if err != nil {
				return err
			}
			rows, err := s.signingSecrets(ctx, tx, endpoint.ID, now)
			if err != nil {
				return err
			}
			category = "signing_unavailable"
			keys, err = s.decryptSecrets(ctx, rows)
			return err
		})
		if err != nil {
			if errorgraph.Is(err, database.NotFound) && delivery.ID.IsZero() {
				return jobs.Permanent(err)
			}
			// A pre-send failure is an attempt too; record it while possible.
			recorded := s.recordAttempt(context.WithoutCancel(ctx), deliveryKey(id), 0, category, Pending)
			return errors.Join(deliveryFailure(category, err), recorded)
		}
		if delivery.State != string(Pending) {
			return nil
		}
		if !endpoint.Active {
			recorded := s.recordAttempt(ctx, delivery.ID, 0, "endpoint_inactive", Failed)
			return jobs.Permanent(errors.Join(fault.New(fault.Conflict, "webhook endpoint is inactive"), recorded))
		}
		stamp := strconv.FormatInt(s.clock.Now().Unix(), 10)
		webhookID := WebhookID(id)
		request := s.client.Post(endpoint.URL).WithBody(httpclient.Bytes([]byte(delivery.Payload))).
			Header("Content-Type", "application/json").Header("User-Agent", webhookUserAgent).
			Header("Webhook-Id", webhookID).Header("Webhook-Timestamp", stamp).
			Header("Webhook-Signature", sign(keys, webhookID, stamp, delivery.Payload)).
			WithRetry(httpclient.NoRetries())
		status := 0
		sendErr := s.client.Stream(ctx, request, func(_ context.Context, response *httpclient.StreamResponse) error {
			status = response.Status()
			// The receiver's answer is its status; a bounded drain keeps the
			// connection reusable and never buffers or stores the body.
			_, _ = io.Copy(io.Discard, io.LimitReader(response, maxResponseDrain))
			return nil
		})
		code, category, retryable := classify(status, sendErr)
		state := Pending
		switch {
		case category == "":
			state = Succeeded
		case !retryable:
			state = Failed
		}
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := s.recordAttempt(recordCtx, delivery.ID, code, category, state); err != nil {
			return err
		}
		switch state {
		case Succeeded:
			return nil
		case Failed:
			return jobs.Permanent(deliveryFailure(category, sendErr))
		}
		return deliveryFailure(category, sendErr)
	})
}

// deliveryFailure keeps the transport cause for errors.Is inspection while its
// message contains only the stable category.
func deliveryFailure(category string, cause error) error {
	return fault.Wrap(fault.Internal, "webhook delivery failed: "+category, cause)
}

// classify maps one attempt to its logged status, failure category and
// whether a later attempt may succeed. A received status decides first: a
// 2xx whose body the client could not read is still delivered.
func classify(status int, err error) (int32, string, bool) {
	var failure *httpclient.Error
	if status == 0 && errors.As(err, &failure) {
		status = failure.Status()
	}
	if status != 0 {
		switch {
		case status >= 200 && status < 300:
			return int32(status), "", false
		case status == 408 || status == 429 || status >= 500:
			return int32(status), "status_retryable", true
		case status < 100 || status > 599:
			return 0, "response_failed", true
		}
		return int32(status), "status_rejected", false
	}
	switch {
	case err == nil:
		return 0, "response_failed", true
	case errorgraph.Is(err, httpclient.DestinationDenied):
		return 0, "destination_denied", false
	case errorgraph.Is(err, fault.Overloaded):
		return 0, "overloaded", true
	case errorgraph.Is(err, context.DeadlineExceeded) || errorgraph.Is(err, context.Canceled):
		return 0, "timeout", true
	case errors.As(err, &failure) && failure.Kind() == httpclient.InvalidRequest:
		return 0, "invalid_request", false
	}
	return 0, "transport_failed", true
}

func (s *Service) recordAttempt(ctx context.Context, id model.ID[store.Delivery], status int32, category string, state DeliveryState) error {
	return s.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
		row, err := store.QueryFoundryWebhookDeliveries().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if row.State == string(Succeeded) || row.State == string(Failed) && state == Pending {
			// A concurrent attempt already settled the delivery; keep it.
			return nil
		}
		now, err := s.now()
		if err != nil {
			return err
		}
		attempts := row.Attempts
		if attempts < 1<<32-1 {
			attempts++
		}
		draft := store.DeliveryDraft{}.SetAttempts(attempts).SetLastStatus(status).SetLastFailure(category).SetState(string(state)).SetUpdatedAt(now)
		if state == Succeeded {
			draft = draft.SetDeliveredAt(now)
		}
		_, err = store.QueryFoundryWebhookDeliveries().Update(ctx, tx, id, draft)
		return err
	})
}

// finalize records a delivery the job runtime gave up on as failed, keeping
// its last attempt's category when one was logged.
func (s *Service) finalize(ctx context.Context, id model.ID[store.Delivery], reason string) error {
	return s.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
		row, err := store.QueryFoundryWebhookDeliveries().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if row.State != string(Pending) {
			return nil
		}
		now, err := s.now()
		if err != nil {
			return err
		}
		category := row.LastFailure
		if category == "" {
			category = reason[:min(len(reason), failureCategoryBytes)]
		}
		_, err = store.QueryFoundryWebhookDeliveries().Update(ctx, tx, id, store.DeliveryDraft{}.SetState(string(Failed)).SetLastFailure(category).SetUpdatedAt(now))
		return err
	})
}

// Replay re-enqueues one delivery with the same webhook-id, so the receiver
// can deduplicate it. The delivery returns to pending; its attempt count and
// last result are kept for inspection.
func (q Queue) Replay(ctx context.Context, id DeliveryID) error {
	if err := q.Validate(); err != nil {
		return err
	}
	if id.IsZero() {
		return invalid()
	}
	return q.service.calls.Run(ctx, "webhook replay", func(ctx context.Context) error {
		return q.service.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
			row, err := store.QueryFoundryWebhookDeliveries().ForUpdate().RequireFind(ctx, tx, deliveryKey(id))
			if err != nil {
				return err
			}
			return q.reset(ctx, tx, row)
		})
	})
}

// ReplayFailed re-enqueues up to limit failed deliveries of active endpoints,
// optionally of one endpoint, in one transaction and reports how many were
// replayed. Deliveries of inactive endpoints are skipped; naming an inactive
// endpoint explicitly is a conflict. The count covers committed work only.
func (q Queue) ReplayFailed(ctx context.Context, endpoint value.Optional[EndpointID], limit int) (int, error) {
	if err := q.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > 1000 {
		return 0, invalid()
	}
	count := 0
	err := q.service.calls.Run(ctx, "webhook failed replay", func(ctx context.Context) error {
		return q.service.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
			count = 0
			e := store.EndpointFields()
			endpoints := store.QueryFoundryWebhookEndpoints().Where(e.Active.Eq(true))
			if id, ok := endpoint.Get(); ok {
				row, err := store.QueryFoundryWebhookEndpoints().RequireFind(ctx, tx, endpointKey(id))
				if err != nil {
					return err
				}
				if !row.Active {
					return fault.New(fault.Conflict, "webhook endpoint is inactive")
				}
				endpoints = endpoints.Where(e.ID.Eq(row.ID))
			}
			active, err := query.SelectValue(endpoints.OrderBy(e.ID.Asc()).Limit(q.service.config.MaxEndpoints+1), e.ID.Value()).All(ctx, tx)
			if err != nil || len(active) == 0 {
				return err
			}
			f := store.DeliveryFields()
			rows, err := store.QueryFoundryWebhookDeliveries().Where(f.State.Eq(string(Failed)), f.EndpointID.In(active...)).OrderBy(f.ID.Asc()).Limit(limit).ForUpdate().All(ctx, tx)
			if err != nil {
				return err
			}
			replayed := 0
			for _, row := range rows {
				if err := q.reset(ctx, tx, row); err != nil {
					return err
				}
				replayed++
			}
			count = replayed
			return nil
		})
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func (q Queue) reset(ctx context.Context, tx *database.Tx, row store.Delivery) error {
	endpoint, err := store.QueryFoundryWebhookEndpoints().RequireFind(ctx, tx, row.EndpointID)
	if err != nil {
		return err
	}
	if !endpoint.Active {
		return fault.New(fault.Conflict, "webhook endpoint is inactive")
	}
	now, err := q.service.now()
	if err != nil {
		return err
	}
	if _, err := store.QueryFoundryWebhookDeliveries().Update(ctx, tx, row.ID, store.DeliveryDraft{}.SetState(string(Pending)).SetUpdatedAt(now)); err != nil {
		return err
	}
	return q.enqueue(ctx, tx, row.ID)
}

// DeliveryQuery selects delivery log entries in ID order.
type DeliveryQuery struct {
	Endpoint value.Optional[EndpointID]
	State    value.Optional[DeliveryState]
	After    DeliveryID
	Limit    int
}

// Deliveries inspects the delivery log without payloads.
func (s *Service) Deliveries(ctx context.Context, selection DeliveryQuery) ([]DeliveryInfo, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if selection.Limit < 1 || selection.Limit > 1000 {
		return nil, invalid()
	}
	if state, ok := selection.State.Get(); ok && state != Pending && state != Succeeded && state != Failed {
		return nil, invalid()
	}
	var result []DeliveryInfo
	err := s.calls.Run(ctx, "webhook delivery inspection", func(ctx context.Context) error {
		return s.transaction(ctx, true, func(ctx context.Context, tx *database.Tx) error {
			f := store.DeliveryFields()
			q := store.QueryFoundryWebhookDeliveries()
			if id, ok := selection.Endpoint.Get(); ok {
				q = q.Where(f.EndpointID.Eq(endpointKey(id)))
			}
			if state, ok := selection.State.Get(); ok {
				q = q.Where(f.State.Eq(string(state)))
			}
			if !selection.After.IsZero() {
				ordered := query.OrderedField[store.Delivery, model.ID[store.Delivery]]{ScalarField: f.ID}
				q = q.Where(ordered.Gt(deliveryKey(selection.After)))
			}
			rows, err := q.OrderBy(f.ID.Asc()).Limit(selection.Limit).All(ctx, tx)
			for _, row := range rows {
				result = append(result, deliveryInfo(row))
			}
			return err
		})
	})
	return result, err
}

// MaxPruneBatch bounds one PruneDeliveries call.
const MaxPruneBatch = 10000

// PruneDeliveries deletes up to limit succeeded or failed deliveries (with
// their stored payloads) last updated more than olderThan ago, and reports how
// many were deleted. Pending deliveries are never pruned. Run it from an
// ordinary scheduled task until it returns fewer than limit.
func (s *Service) PruneDeliveries(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if olderThan < time.Hour || limit < 1 || limit > MaxPruneBatch {
		return 0, invalid()
	}
	count := 0
	err := s.calls.Run(ctx, "webhook delivery pruning", func(ctx context.Context) error {
		return s.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
			now, err := s.now()
			if err != nil {
				return err
			}
			cutoff, err := temporal.NewDateTime(now.UTC().Add(-olderThan))
			if err != nil {
				return err
			}
			f := store.DeliveryFields()
			ids, err := query.SelectValue(store.QueryFoundryWebhookDeliveries().Where(f.State.In(string(Succeeded), string(Failed)), f.UpdatedAt.Lt(cutoff)).OrderBy(f.UpdatedAt.Asc(), f.ID.Asc()).Limit(limit), f.ID.Value()).All(ctx, tx)
			if err != nil || len(ids) == 0 {
				return err
			}
			deleted, err := store.QueryFoundryWebhookDeliveries().Where(f.ID.In(ids...), f.State.In(string(Succeeded), string(Failed))).DeleteAll(ctx, tx)
			count = int(deleted)
			return err
		})
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
