package background_test

import (
	"context"
	"foundry.test/consumer/background"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

func TestCapturedWelcomeHasStableTypedIdentity(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	declaration, err := background.WelcomeJob.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	userID, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := background.WelcomeJob.Capture(context.Background(), background.Welcome{UserID: userID}, jobs.Options[background.Welcome]{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := pending.Dispatch(context.Background(), dispatcher)
	if err != nil || !first.Inserted {
		t.Fatalf("dispatch: %v", err)
	}
	second, err := pending.Dispatch(context.Background(), dispatcher)
	if err != nil || second.Inserted || second.ID != first.ID {
		t.Fatalf("duplicate dispatch: %v", err)
	}
	found, err := background.WelcomeJob.Inspect(context.Background(), dispatcher, first.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	record, ok := found.Get()
	if !ok || record.State != jobs.Waiting {
		t.Fatal("job not waiting")
	}
}
