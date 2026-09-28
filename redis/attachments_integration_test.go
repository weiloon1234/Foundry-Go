package redis

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

type attachmentRetryStorage struct {
	storage.Backend
	calls atomic.Int32
}

func (s *attachmentRetryStorage) Delete(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
	if s.calls.Add(1) <= 2 {
		return storage.Failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, nil)
	}
	return s.Backend.Delete(ctx, key, options)
}

// This composes real PostgreSQL ownership/outbox transactions, Redis durable
// publication, a worker retry, and conditional local object deletion.
func TestRedisAttachmentReconciliationOutboxAndWorker(t *testing.T) {
	client, namespace, track := integrationAddresses(t, nil)
	queueKey, err := jobs.NewKey(namespace, "attachments")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range jobKeys(queueKey) {
		track(key)
	}
	backend, err := NewJobBackend(client, jobs.DefaultQueueConfig())
	if err != nil {
		t.Fatal(err)
	}
	f := extensiontest.Open(t, append(attachments.Migrations(), outbox.Migrations()...))
	localBackend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	retryStorage := &attachmentRetryStorage{Backend: localBackend}
	diskDeclaration := storage.DefineDisk("attachment-job-files")
	disk, err := diskDeclaration.Bind(retryStorage, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := disk.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := localBackend.Close(); err != nil {
			t.Error(err)
		}
	})
	disks, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	collection := attachments.Define(extensiontest.Members, "document", attachments.Policy{Disk: diskDeclaration, Cardinality: attachments.Single, Accepted: []storage.MediaType{"text/plain"}})
	manager, err := attachments.New(attachments.Dependencies{Store: f.Store, Disks: disks}, attachments.DefaultConfig(), collection.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	policy := jobs.DefaultPolicy("attachments")
	policy.Backoff = []time.Duration{time.Millisecond}
	policy.Jitter = 0
	job := attachments.DefineReconcileJob("attachments.reconcile", policy)
	declaration, err := job.Declare(manager)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("attachments", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := job.ToOutbox(producer)
	if err != nil {
		t.Fatal(err)
	}
	collection = collection.WithReconciliation(queue)
	route, err := producer.PublicationRoute()
	if err != nil {
		t.Fatal(err)
	}
	writer := outboxtest.Writer{DB: f.DB, Schema: f.Schema}
	publication, err := publisher.New(writer, publisher.DefaultConfig(), route)
	if err != nil {
		t.Fatal(err)
	}
	owner := extensiontest.Members.Reference(1)
	first, err := collection.Add(t.Context(), manager, owner, attachments.Upload{Source: strings.NewReader("old")})
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("parent rollback")
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		if err := attachments.Cleanup(ctx, tx, manager, extensiontest.Members, owner, lifecycle.Delete, &queue); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if result, err := publication.PublishOne(t.Context()); err != nil || result.Found {
		t.Fatal("rolled back attachment job escaped", err)
	}
	if retryStorage.calls.Load() != 0 {
		t.Fatal("rollback deleted storage")
	}
	second, err := collection.Replace(t.Context(), manager, owner, attachments.Upload{Source: strings.NewReader("new")})
	if err == nil || second.Publication != attachments.Published || len(second.PendingCleanup) != 1 {
		t.Fatal("replacement did not retain cleanup job", err)
	}
	if result, err := publication.PublishOne(t.Context()); err != nil || !result.Found || result.Failure != nil {
		t.Fatal("cleanup publication", err)
	}
	config := jobs.DefaultWorkerConfig(namespace, "attachments")
	config.Concurrency = 1
	config.PollInterval = time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err := manager.Inspect(t.Context(), first.Operation)
		if err != nil {
			t.Fatal(err)
		}
		if state.State == attachments.Cleaned {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("attachment reconciliation worker did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if retryStorage.calls.Load() != 3 {
		t.Fatal("worker did not retry failed conditional cleanup", retryStorage.calls.Load())
	}
	current, err := collection.First(t.Context(), manager, owner)
	file, ok := current.Get()
	if err != nil || !ok {
		t.Fatal("replacement ownership lost", err)
	}
	body, err := collection.ReadBytes(t.Context(), manager, owner, file.ID(), 1024)
	if err != nil || string(body) != "new" {
		t.Fatal("worker deleted current file", err)
	}
}
