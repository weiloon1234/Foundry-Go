package teamworkflow

import (
	"bytes"
	"context"
	"encoding/json"
	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedPatchPresenceAndNamedIsolation(t *testing.T) {
	for _, suffix := range []string{"first", "second"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			main, receiver := pgtest.Isolate(t), pgtest.Isolate(t)
			app := startApp(t, main, receiver, Hooks{})
			db, err := app.app.Resources().Database()
			if err != nil {
				t.Fatal(err)
			}
			named, err := app.app.Resources().Databases.Connection("main")
			if err != nil || named != db {
				t.Fatal("default database is not the named default", err)
			}
			receiving, err := app.app.Resources().Databases.Connection("receiver")
			if err != nil || receiving == db {
				t.Fatal("named receiver did not keep its concrete pool", err)
			}
			path := "/teams/1/projects/shared"
			cases := []struct {
				body   string
				title  *string
				budget int64
			}{
				{`{}`, pointer("Initial"), 10},
				{`{"title":null}`, nil, 10},
				{`{"title":"","budget":0}`, pointer(""), 0},
				{`{"title":"  ` + suffix + `  ","budget":17}`, pointer(suffix), 17},
				{`{"budget":0}`, pointer(suffix), 0},
			}
			for _, tc := range cases {
				response := send(t, app, "PATCH", path, "alice", "", tc.body, 200)
				envelope, err := httptest.DecodeJSON(t.Context(), response, genericdto.EnvelopeJSON(ActionJSON()))
				if err != nil {
					t.Fatal(err)
				}
				project, ok := envelope.Data.Updated()
				if !ok {
					t.Fatal("PATCH lost typed action discrimination")
				}
				title, present := project.Title.Get()
				if present != (tc.title != nil) || present && title != *tc.title || project.Budget != tc.budget {
					t.Fatal("PATCH response changed presence")
				}
				stored, err := QueryWorkflowProjects().Where(ProjectFields().TeamID.Eq(1), ProjectFields().Slug.Eq("shared")).RequireFirst(t.Context(), db)
				if err != nil {
					t.Fatal(err)
				}
				value, valid := stored.Title.Get()
				if valid != present || value != title || stored.Budget != tc.budget {
					t.Fatal("HTTP did not update its real scoped database")
				}
			}
			send(t, app, "PATCH", path, "alice", "", `{"title":12}`, 400)
			send(t, app, "PATCH", path, "alice", "", `{"budget":-1}`, 422)
			send(t, app, "PATCH", path, "invalid", "", `{}`, 401)
			send(t, app, "PATCH", "/teams/1/projects/foreign", "alice", "", `{}`, 404)
			send(t, app, "PATCH", "/teams/2/projects/shared", "alice", "", `{}`, 403)
			send(t, app, "GET", path, "alice", "", "", 200)
			other, err := QueryWorkflowProjects().Where(ProjectFields().TeamID.Eq(2), ProjectFields().Slug.Eq("shared")).RequireFirst(t.Context(), db)
			if err != nil || other.Budget != 99 {
				t.Fatal("same slug under another parent was changed", err)
			}
			page := send(t, app, "GET", "/catalogue?team=1&page=1&per_page=1", "", "", "", 200)
			var payload struct {
				Data []CatalogueItem `json:"data"`
				Meta struct {
					Total int64 `json:"total"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(page.Bytes(), &payload); err != nil || len(payload.Data) != 1 || payload.Meta.Total != 1 {
				t.Fatal("typed numbered catalogue mismatch", err)
			}
			if scalar(t, receiving, `SELECT total FROM workflow_delivery_totals WHERE name='submissions'`) != 0 {
				t.Fatal("PATCH unexpectedly published a submission")
			}
			app.stop()
			retained := main.Open(t)
			row, err := QueryWorkflowProjects().Where(ProjectFields().TeamID.Eq(1)).RequireFirst(t.Context(), retained)
			if err != nil {
				t.Fatal(err)
			}
			title, _ := row.Title.Get()
			if title != suffix {
				t.Fatal("shutdown lost retained isolated data")
			}
		})
	}
}
func pointer(value string) *string { return &value }

func TestDuplicateSubmissionAndReceivingTransaction(t *testing.T) {
	main, receiver := pgtest.Isolate(t), pgtest.Isolate(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	delivered := make(chan publisher.Message, 8)
	hooks := Hooks{Calls: &calls, InTransaction: func(ctx context.Context, _ *database.Tx) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, Delivered: func(message publisher.Message) {
		select {
		case delivered <- message:
		default:
		}
	}}
	first, second := startApp(t, main, receiver, hooks), startApp(t, main, receiver, hooks)
	path, key := "/teams/1/projects/shared/submissions", "integrated-submission-0001"
	type result struct {
		response httpclient.Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		r, e := first.send(t.Context(), "POST", path, "alice", key, "application/json", []byte(`{"name":" Example "}`))
		done <- result{r, e}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("submission did not enter transaction")
	}
	waiting := send(t, second, "POST", path, "alice", key, `{"name":"Example"}`, 409)
	if !bytes.Contains(waiting.Bytes(), []byte("idempotency_in_progress")) {
		unblock()
		t.Fatal("duplicate did not preserve live claim")
	}
	unblock()
	original := <-done
	if original.err != nil || original.response.Status() != 202 {
		t.Fatal("submission failed", original.err)
	}
	replay := send(t, second, "POST", path, "alice", key, `{"name":"Example"}`, 202)
	if !bytes.Equal(original.response.Bytes(), replay.Bytes()) || calls.Load() != 1 {
		t.Fatal("replay changed tagged envelope or repeated writes")
	}
	db, _ := first.app.Resources().Database()
	if scalar(t, db, `SELECT count(*) FROM workflow_submissions`) != 1 || scalar(t, db, `SELECT count(*) FROM foundry_outbox`) != 1 {
		t.Fatal("business/outbox atomicity failed")
	}
	send(t, first, "POST", path, "alice", key, `{"name":"Different"}`, 409)
	send(t, first, "POST", path, "invalid", key, `{"name":"Example"}`, 401)
	send(t, first, "POST", path, "alice", "rollback-submission-0001", `{"name":"Reject","reject":true}`, 422)
	if scalar(t, db, `SELECT count(*) FROM workflow_submissions`) != 1 || scalar(t, db, `SELECT count(*) FROM foundry_outbox`) != 1 {
		t.Fatal("domain rejection left effects")
	}
	var message publisher.Message
	select {
	case message = <-delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("managed publisher did not reach receiver")
	}
	receiving, _ := first.app.Resources().Databases.Connection("receiver")
	sink := NewDeliverySink(receiving)
	failures := make(chan error, 2)
	for range 2 {
		go func() { failures <- sink.Deliver(t.Context(), message) }()
	}
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if scalar(t, receiving, `SELECT count(*) FROM workflow_delivery_receipts`) != 1 || scalar(t, receiving, `SELECT total FROM workflow_delivery_totals WHERE name='submissions'`) != 1 {
		t.Fatal("duplicate delivery repeated receiving effect")
	}
	waitFor(t, func() bool {
		return scalar(t, db, `SELECT count(*) FROM foundry_outbox WHERE publish_state='published'`) == 1
	})
	project, err := QueryWorkflowProjects().Where(ProjectFields().TeamID.Eq(1)).RequireFirst(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := QueryWorkflowProjects().Update(t.Context(), db, project.ID, ProjectDraft{}.SetEnabled(false)); err != nil {
		t.Fatal(err)
	}
	send(t, second, "POST", path, "alice", key, `{"name":"Example"}`, 403)
	// The source and named receiver each keep their data after ordinary teardown.
	first.stop()
	second.stop()
	if scalar(t, main.Open(t), `SELECT count(*) FROM workflow_submissions`) != 1 || scalar(t, receiver.Open(t), `SELECT total FROM workflow_delivery_totals WHERE name='submissions'`) != 1 {
		t.Fatal("shutdown destroyed an accepted effect")
	}
}
