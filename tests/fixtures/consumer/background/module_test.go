package background_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"foundry.test/consumer/background"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

type welcomeSender struct{ calls chan model.ID[models.User] }

func (s welcomeSender) SendWelcome(_ context.Context, id model.ID[models.User]) error {
	s.calls <- id
	return nil
}
func TestWorkerKernelRunsConsumerHandlerWithTypedService(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	sender := welcomeSender{calls: make(chan model.ID[models.User], 1)}
	app, err := foundry.New().Register(background.WorkerModule(sender, backend, keyspace.Namespace{Application: "consumer", Environment: "test"})).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := foundation.Resolve(app.Services(), background.DispatcherKey)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := background.DispatchWelcome(t.Context(), dispatcher, id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.Worker) }()
	select {
	case got := <-sender.calls:
		if got != id {
			t.Fatal("user ID changed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker kernel did not execute")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		found, err := background.WelcomeJob.Inspect(t.Context(), dispatcher, receipt.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if record, ok := found.Get(); ok && record.State == jobs.Succeeded {
			cancel()
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("worker kernel did not drain")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("worker did not acknowledge")
}
