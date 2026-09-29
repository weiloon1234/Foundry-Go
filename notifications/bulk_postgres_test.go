package notifications

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

func TestOnDemandRoutesDeliverWithoutARecipientModel(t *testing.T) {
	var mu sync.Mutex
	var delivered []string
	channel := Custom("sms", textSchema[InboxData](), func(_ context.Context, route Route, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{route.Address + ":" + p.Text}, nil
	}, transportFunc[InboxData](func(_ context.Context, _ DeliveryID, data InboxData) (Outcome, error) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, data.Text)
		return Accepted, nil
	}))
	b := BindOnDemand(Define("guest.invited", 1, textSchema[Input]()), channel)
	m, _ := fixture(t, b.Registration())
	guest, err := email.ParseAddress("guest@example.test")
	if err != nil {
		t.Fatal(err)
	}
	route := Route{Email: guest, Address: "+15550100"}
	reference, err := route.Reference()
	if err != nil {
		t.Fatal(err)
	}
	report, err := b.Send(t.Context(), m, reference, Input{"welcome"})
	if err != nil || report.Channels[0].State != Delivered {
		t.Fatal("on-demand notification not delivered", err)
	}
	if len(delivered) != 1 || delivered[0] != "+15550100:welcome" {
		t.Fatal("route not passed to the renderer", delivered)
	}
	if _, err := (Route{}).Reference(); err == nil {
		t.Fatal("empty route accepted")
	}
	if err := BindOnDemand(Define("guest.inbox", 1, textSchema[Input]()), Database[Route, Input, InboxData]("inbox", textSchema[InboxData](), func(context.Context, Route, DeliveryContext, Input) (InboxData, error) {
		return InboxData{}, nil
	}).Channel()).Validate(); err == nil {
		t.Fatal("on-demand binding accepted an inbox channel")
	}
}

func TestBulkEnqueueStoresBatchesAndPruneRemovesOldInboxRecords(t *testing.T) {
	a := newAuthority(t)
	for key := int64(1); key <= 5; key++ {
		a.set(Member{ID: key, Enabled: true, Allowed: true, Email: "member@example.test"})
	}
	var sends atomic.Int32
	b := Bind(Define("digest.weekly", 1, textSchema[Input]()), a.recipient, databaseChannel().Channel())
	m, writer := fixture(t, b.Registration())
	policy := jobs.DefaultPolicy("notifications")
	policy.Jitter = 0
	job := DefineDeliveryJob("notifications.bulk", policy)
	declaration, err := job.Declare(m)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	dispatcher, err := jobs.NewDispatcher(&durableQueue{backend}, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "notifications", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("notifications", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	var recipients []model.Reference[Member, int64]
	for key := int64(1); key <= 5; key++ {
		recipients = append(recipients, Member{ID: key}.FoundryReference())
	}
	result, err := b.EnqueueMany(t.Context(), m, job, producer, recipients, Input{"digest"}, 2)
	if err != nil || len(result.Stored) != 5 || len(result.Unconfirmed) != 0 {
		t.Fatal("bulk enqueue", len(result.Stored), err)
	}
	route, err := producer.PublicationRoute()
	if err != nil {
		t.Fatal(err)
	}
	config := publisher.DefaultConfig()
	config.MaxInFlight = 16
	p, err := publisher.New(writer, config, route)
	if err != nil {
		t.Fatal(err)
	}
	if results, err := p.PublishBatch(t.Context()); err != nil || len(results) != 5 {
		t.Fatal("delivery jobs were not stored with their notifications", len(results), err)
	}
	// Deliver every stored notification, then prune the inbox records.
	for _, id := range result.Stored {
		if _, err := m.Deliver(t.Context(), model.IDFromBytes[Notification](id.Bytes())); err != nil {
			t.Fatal(err)
		}
		sends.Add(1)
	}
	inbox, err := a.recipient.Inbox(m)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := inbox.List(a.context(t, 1), query.PageRequest{Number: 1, Size: 10}, false); err != nil || page.Total != 1 {
		t.Fatal("bulk notification not delivered", err)
	}
	if _, err := inbox.MarkAllRead(a.context(t, 1)); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if deleted, err := m.PruneInbox(t.Context(), future, true, 10); err != nil || deleted != 1 {
		t.Fatal("read-only prune", deleted, err)
	}
	if deleted, err := m.PruneInbox(t.Context(), time.Now().Add(-time.Hour), false, 10); err != nil || deleted != 0 {
		t.Fatal("prune removed recent records", deleted, err)
	}
	if deleted, err := m.PruneInbox(t.Context(), future, false, 3); err != nil || deleted != 3 {
		t.Fatal("bounded prune batch", deleted, err)
	}
	if deleted, err := m.PruneInbox(t.Context(), future, false, 10); err != nil || deleted != 1 {
		t.Fatal("prune remainder", deleted, err)
	}
	if sends.Load() != 5 {
		t.Fatal("deliveries", sends.Load())
	}
}

func TestBulkEnqueueReportsFailedBatchIDsForReconciliation(t *testing.T) {
	a := newAuthority(t)
	for key := int64(1); key <= 4; key++ {
		a.set(Member{ID: key, Enabled: true, Allowed: true})
	}
	b := Bind(Define("digest.retry", 1, textSchema[Input]()), a.recipient, databaseChannel().Channel())
	m, _ := fixture(t, b.Registration())
	job := DefineDeliveryJob("notifications.bulk.retry", jobs.DefaultPolicy("notifications"))
	declaration, err := job.Declare(m)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	dispatcher, err := jobs.NewDispatcher(&durableQueue{backend}, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "notifications", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("notifications", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	var recipients []model.Reference[Member, int64]
	ids := make([]ID[Member], 4)
	for key := int64(1); key <= 4; key++ {
		recipients = append(recipients, Member{ID: key}.FoundryReference())
		id, err := NewID[Member]()
		if err != nil {
			t.Fatal(err)
		}
		ids[key-1] = id
	}
	// A notification already stored under ids[2] with different input makes
	// the third batch fail: its IDs are reported, never lost.
	if _, err := b.EnqueueManyWithIDs(t.Context(), m, job, producer, recipients[2:3], ids[2:3], Input{"other"}, 1); err != nil {
		t.Fatal(err)
	}
	result, err := b.EnqueueManyWithIDs(t.Context(), m, job, producer, recipients, ids, Input{"digest"}, 1)
	if err == nil || len(result.Stored) != 2 || result.Stored[0] != ids[0] || result.Stored[1] != ids[1] ||
		len(result.Unconfirmed) != 1 || result.Unconfirmed[0] != ids[2] || len(result.NotAttempted) != 1 || result.NotAttempted[0] != ids[3] {
		t.Fatal("failed batch IDs not reported", result, err)
	}
	// Retrying with the same IDs is idempotent for the stored notifications.
	retry := []model.Reference[Member, int64]{recipients[0], recipients[1], recipients[3]}
	retryIDs := []ID[Member]{ids[0], ids[1], ids[3]}
	for range 2 {
		result, err = b.EnqueueManyWithIDs(t.Context(), m, job, producer, retry, retryIDs, Input{"digest"}, 2)
		if err != nil || len(result.Stored) != 3 || len(result.Unconfirmed)+len(result.NotAttempted) != 0 {
			t.Fatal("retry with supplied IDs", result, err)
		}
	}
	for i, id := range result.Stored {
		if id != retryIDs[i] {
			t.Fatal("supplied IDs were not used in order")
		}
	}
	pending, err := m.Deliveries(t.Context(), Pending, MaxDeliveryPage, DeliveryID{})
	if err != nil || len(pending) != len(ids) {
		t.Fatal("each notification must be stored exactly once", len(pending), err)
	}
	stored := map[NotificationID]bool{}
	for _, delivery := range pending {
		stored[delivery.Notification] = true
	}
	for _, id := range ids {
		if !stored[model.IDFromBytes[Notification](id.Bytes())] {
			t.Fatal("notification not stored")
		}
	}
	if _, err := b.EnqueueManyWithIDs(t.Context(), m, job, producer, recipients, ids[:1], Input{"digest"}, 2); err == nil {
		t.Fatal("mismatched ID count accepted")
	}
}
