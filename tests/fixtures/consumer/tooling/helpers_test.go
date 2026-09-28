package tooling_test

import (
	"context"
	"testing"
	"time"

	"foundry.test/consumer/background"
	"foundry.test/consumer/eventqueries"
	"foundry.test/consumer/models"
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/testkit"
	emailtest "github.com/weiloon1234/Foundry-Go/testkit/email"
	eventstest "github.com/weiloon1234/Foundry-Go/testkit/events"
	jobstest "github.com/weiloon1234/Foundry-Go/testkit/jobs"
	storagetest "github.com/weiloon1234/Foundry-Go/testkit/storage"
)

func TestLocalCapabilitiesUseConcreteConsumerContracts(t *testing.T) {
	source := testkit.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	declaration, err := background.WelcomeJob.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	queue := jobstest.New(t, source, declaration)
	user, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := background.WelcomeJob.Capture(t.Context(), background.Welcome{UserID: user}, jobs.Options[background.Welcome]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Dispatch(t.Context(), queue.Dispatcher); err != nil {
		t.Fatal(err)
	}
	jobstest.AssertState(t, background.WelcomeJob, queue.Dispatcher, pending.ID(), "", jobs.Waiting)

	recorder, err := eventstest.New[eventqueries.RecordCreated](4)
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventqueries.Created.Declare(recorder.Listener("tooling.record"))
	if err != nil {
		t.Fatal(err)
	}
	bus := eventstest.Start(t, event)
	id, err := model.NewID[observerqueries.Plain]()
	if err != nil {
		t.Fatal(err)
	}
	if err := eventqueries.Created.Dispatch(t.Context(), bus, eventqueries.RecordCreated{ID: id}); err != nil {
		t.Fatal(err)
	}
	eventstest.AssertCount(t, recorder, 1)
	values, err := recorder.Payloads(t.Context())
	if err != nil || len(values) != 1 || values[0].ID != id {
		t.Fatal("consumer event owner changed", err)
	}

	disk := storagetest.Local(t, "artifacts")
	key, err := storage.ParseKey("reports/one.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.PutBytes(t.Context(), key, []byte("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	data, _, err := disk.ReadBytes(t.Context(), key, 64, storage.ReadOptions{})
	if err != nil || string(data) != "report" {
		t.Fatal("test disk changed content", err)
	}

	driver := emailtest.New(t, 4)
	mailer, err := email.New(driver, nil, email.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := mailer.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	sender, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := email.ParseAddress("recipient@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mailer.Send(t.Context(), email.NewMessage(sender, "Report ready", recipient).Text("Ready"), email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	emailtest.AssertRecipientCount(t, driver, recipient, 1)
}
