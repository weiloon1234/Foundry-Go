package email_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	jobmemory "github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

type queuedWelcome struct {
	Recipient   email.Address      `json:"recipient"`
	Name        string             `json:"name"`
	Attachments []email.Attachment `json:"attachments,omitempty"`
}
type emailQueue struct {
	definition jobs.Definition[queuedWelcome]
	dispatcher *jobs.Dispatcher
	worker     *jobs.Worker
}

type queueOptions struct {
	timeout    time.Duration
	middleware []jobs.Middleware[queuedWelcome]
}

func prepareQueue(t *testing.T, backend jobs.Backend, m *email.Mailer, options ...queueOptions) emailQueue {
	t.Helper()
	from := address(t, "sender@example.test")
	policy := jobs.DefaultPolicy("mail")
	policy.Backoff = []time.Duration{time.Millisecond}
	policy.Jitter = 0
	policy.Attempts = 3
	var hooks jobs.HandlerOptions[queuedWelcome]
	if len(options) > 0 {
		if options[0].timeout > 0 {
			policy.Timeout = options[0].timeout
		}
		hooks.Middleware = options[0].middleware
	}
	definition := jobs.Define[queuedWelcome]("welcome.email", 1, policy)
	declaration, err := definition.DeclareWith(email.JobHandler(m, func(_ context.Context, p queuedWelcome) (email.Message, error) {
		return email.NewMessage(from, "Welcome "+p.Name, p.Recipient).Text("Welcome " + p.Name).Attach(p.Attachments...), nil
	}), hooks)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	namespace := keyspace.Namespace{Application: "email", Environment: "test"}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(namespace, "mail")
	config.Concurrency = 1
	config.PollInterval = time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	return emailQueue{definition, dispatcher, worker}
}
func startQueue(t *testing.T, q emailQueue) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- q.worker.Run(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := q.worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("email worker did not stop")
		}
	})
}
func waitEmailJob(t *testing.T, q emailQueue, id jobs.ID[queuedWelcome], want jobs.State) jobs.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		record, err := q.definition.Inspect(ctx, q.dispatcher, id, "")
		if err != nil {
			t.Fatal(err)
		}
		if value, ok := record.Get(); ok && value.State == want {
			return value
		}
		select {
		case <-ctx.Done():
			t.Fatal("email job did not reach expected state")
		case <-ticker.C:
		}
	}
}
func TestQueuedEmailTypedSnapshotStableKeyAndRetryClassification(t *testing.T) {
	for _, kind := range []email.Kind{email.Transient, email.Permanent, email.Construction, email.Ambiguous} {
		t.Run(string(kind), func(t *testing.T) {
			var mu sync.Mutex
			var attempts []email.Outbound
			m := mailer(t, email.DriverFunc(func(_ context.Context, out email.Outbound) (email.Receipt, error) {
				mu.Lock()
				defer mu.Unlock()
				attempts = append(attempts, out)
				if len(attempts) == 1 {
					return email.Receipt{}, kind
				}
				return email.Receipt{}, nil
			}), nil, nil)
			backend, err := jobmemory.New(jobmemory.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = backend.Close() })
			q := prepareQueue(t, backend, m)
			input := queuedWelcome{Recipient: address(t, "recipient@example.test"), Name: "original"}
			pending, err := q.definition.Capture(t.Context(), input, jobs.Options[queuedWelcome]{})
			if err != nil {
				t.Fatal(err)
			}
			input.Name = "mutated"
			if strings.Contains(pending.Envelope().PayloadJSON(), "mutated") {
				t.Fatal("queued DTO mutated")
			}
			if _, err := pending.Dispatch(t.Context(), q.dispatcher); err != nil {
				t.Fatal(err)
			}
			startQueue(t, q)
			state, wantAttempts := jobs.Failed, 1
			if kind == email.Transient {
				state, wantAttempts = jobs.Succeeded, 2
			}
			record := waitEmailJob(t, q, pending.ID(), state)
			mu.Lock()
			defer mu.Unlock()
			if int(record.Attempts) != wantAttempts || len(attempts) != wantAttempts {
				t.Fatal("incorrect retry decision", record.Attempts, len(attempts))
			}
			for _, out := range attempts {
				if string(out.IdempotencyKey()) != "email/"+pending.ID().String() || out.Message().Subject() != "Welcome original" {
					t.Fatal("queued key/data changed")
				}
			}
		})
	}
}
func TestQueuedAttachmentsRequireStableVersion(t *testing.T) {
	registry, attachment := attachmentStore(t)
	_, d := memoryMailer(t)
	m := mailer(t, d, registry, nil)
	backend, err := jobmemory.New(jobmemory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	q := prepareQueue(t, backend, m)
	attachment.IfMatch = ""
	id, err := q.definition.Dispatch(t.Context(), q.dispatcher, queuedWelcome{Recipient: address(t, "recipient@example.test"), Attachments: []email.Attachment{attachment}}, jobs.Options[queuedWelcome]{})
	if err != nil {
		t.Fatal(err)
	}
	startQueue(t, q)
	record := waitEmailJob(t, q, id.ID, jobs.Failed)
	if record.Attempts != 1 || len(d.Messages()) != 0 {
		t.Fatal("unversioned attachment reached transport")
	}
}

// Fault-injection durable adapter; Redis durability is verified by the jobs
// milestone. This fixture exercises real PostgreSQL commit visibility here.
type emailDurablePeer struct{ *jobmemory.Backend }

func (*emailDurablePeer) DurableAcceptance() bool { return true }
func TestEmailOutboxSuppressesRollbackAndSendsCommittedDTO(t *testing.T) {
	writer := outboxtest.Open(t)
	m, d := memoryMailer(t)
	backend, err := jobmemory.New(jobmemory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	q := prepareQueue(t, &emailDurablePeer{backend}, m)
	producer, err := jobs.PrepareOutbox("mail", q.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	route, err := producer.PublicationRoute()
	if err != nil {
		t.Fatal(err)
	}
	publishing, err := publisher.New(writer, publisher.DefaultConfig(), route)
	if err != nil {
		t.Fatal(err)
	}
	input := queuedWelcome{Recipient: address(t, "recipient@example.test"), Name: "committed"}
	rollback := errors.New("rollback")
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := q.definition.Enqueue(t.Context(), tx, producer, input, jobs.Options[queuedWelcome]{}); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if publication, err := publishing.PublishOne(t.Context()); err != nil || publication.Found {
		t.Fatal("rollback became publishable", err)
	}
	pending, err := q.definition.Capture(t.Context(), input, jobs.Options[queuedWelcome]{})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := pending.Enqueue(t.Context(), tx, producer)
		if err != nil {
			return err
		}
		publication, err := publishing.PublishOne(t.Context())
		if err != nil {
			return err
		}
		if publication.Found {
			return errors.New("uncommitted email visible")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if publication, err := publishing.PublishOne(t.Context()); err != nil || !publication.Committed {
		t.Fatal("committed email not published", err)
	}
	startQueue(t, q)
	waitEmailJob(t, q, pending.ID(), jobs.Succeeded)
	if len(d.Messages()) != 1 || d.Messages()[0].Message().Subject() != "Welcome committed" {
		t.Fatal("committed email not delivered once")
	}
}

func TestQueuedAcceptancePreventsRetryAfterTimeoutAndMiddlewareFailure(t *testing.T) {
	for _, mode := range []string{"timeout", "after-hook"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			m := mailer(t, email.DriverFunc(func(ctx context.Context, _ email.Outbound) (email.Receipt, error) {
				calls.Add(1)
				if mode == "timeout" {
					<-ctx.Done()
				}
				return email.Receipt{}, nil
			}), nil, nil)
			backend, err := jobmemory.New(jobmemory.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = backend.Close() })
			options := queueOptions{}
			if mode == "timeout" {
				options.timeout = 10 * time.Millisecond
			} else {
				options.middleware = []jobs.Middleware[queuedWelcome]{{After: func(context.Context, queuedWelcome) error { return errors.New("after hook failed") }}}
			}
			q := prepareQueue(t, backend, m, options)
			receipt, err := q.definition.Dispatch(t.Context(), q.dispatcher, queuedWelcome{Recipient: address(t, "recipient@example.test")}, jobs.Options[queuedWelcome]{})
			if err != nil {
				t.Fatal(err)
			}
			startQueue(t, q)
			record := waitEmailJob(t, q, receipt.ID, jobs.Failed)
			if record.Attempts != 1 || calls.Load() != 1 || m.Snapshot().Accepted != 1 {
				t.Fatal("accepted email retried after job failure")
			}
		})
	}
}
