package background_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/background"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

type retrySender struct{ calls atomic.Int32 }

func (s *retrySender) SendWelcome(context.Context, model.ID[models.User]) error {
	if s.calls.Add(1) == 1 {
		return jobs.Permanent(errors.New("private-provider-error"))
	}
	return nil
}

func TestConfiguredWorkerLogsAndCommandsRetrySelectedConnection(t *testing.T) {
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Worker.Enabled = true
	s.Worker.Connection = "background"
	s.Worker.Config.PollInterval = time.Millisecond
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": infrastructure.DefaultJobConnectionSettings(), "background": infrastructure.DefaultJobConnectionSettings()}
	selected := s.Services.Jobs.Connections["background"]
	selected.DefaultQueue = "communications"
	s.Services.Jobs.Connections["background"] = selected
	var logs bytes.Buffer
	sender := &retrySender{}
	var failures atomic.Int32
	registration := background.ConfiguredWelcome(sender, jobs.Middleware[background.Welcome]{Failed: func(ctx context.Context, input background.Welcome, err error) error {
		if _, ok := background.WelcomeJob.CurrentID(ctx); !ok || input.UserID.IsZero() || err == nil {
			return errors.New("missing typed failure context")
		}
		failures.Add(1)
		return nil
	}})
	app, err := application.New(s, application.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))).Jobs(registration, registration.On("background")).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.Worker) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("configured worker failed to drain")
		}
	})
	connection, err := app.Resources().Jobs.Connection("background")
	if err != nil {
		t.Fatal(err)
	}
	job, err := background.WelcomeJob.On(connection)
	if err != nil {
		t.Fatal(err)
	}
	user, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := job.Dispatch(t.Context(), background.Welcome{UserID: user}, jobs.Options[background.Welcome]{})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(state jobs.State) jobs.Record {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			found, err := job.Inspect(t.Context(), receipt.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if r, ok := found.Get(); ok && r.State == state {
				return r
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("job did not reach state", state)
		return jobs.Record{}
	}
	wait(jobs.Failed)
	command, err := background.JobCommands()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(command)
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		invocation, err := registry.Parse(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if err := invocation.Run(t.Context(), app.Services(), cli.Streams{In: strings.NewReader(""), Out: &out, Err: io.Discard}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	anonymous := run("jobs", "failed", "--format", "json")
	if strings.Contains(anonymous, receipt.ID.String()) {
		t.Fatal("failed job leaked into default connection")
	}
	text := run("jobs", "inspect", "--connection", "background", "--id", receipt.ID.String(), "--format", "json")
	var inspection struct {
		Job jobs.Summary `json:"job"`
	}
	if err := json.Unmarshal([]byte(text), &inspection); err != nil {
		t.Fatal(err)
	}
	if inspection.Job.RetryToken == "" || strings.Contains(text, user.String()) || strings.Contains(text, "private-provider") {
		t.Fatal("inspection lost token or exposed private data")
	}
	retryArgs := []string{"jobs", "retry", "--connection", "background", "--id", receipt.ID.String(), "--token", string(inspection.Job.RetryToken), "--format", "json"}
	if !strings.Contains(run(retryArgs...), `"changed":true`) {
		t.Fatal("retry not applied")
	}
	record := wait(jobs.Succeeded)
	if record.Retries != 1 || record.Attempts != 1 || sender.calls.Load() != 2 || failures.Load() != 1 {
		t.Fatal("configured retry or middleware did not run")
	}
	if !strings.Contains(run(retryArgs...), `"changed":false`) {
		t.Fatal("repeated command replayed work")
	}
	// Operators read queue depth without payloads; the retried job is retained.
	var depth jobs.QueueStats
	if err := json.Unmarshal([]byte(run("jobs", "stats", "--connection", "background", "--queue", "communications", "--format", "json")), &depth); err != nil || depth.Retained != 1 || depth.Failed != 0 || depth.Waiting != 0 {
		t.Fatalf("queue stats: %+v %v", depth, err)
	}
	cancel()
	// Wait for the application rather than merely observing queue completion;
	// finalization logging and logger ownership are part of shutdown.
	select {
	case <-app.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("application did not close")
	}
	text = logs.String()
	if !strings.Contains(text, `"msg":"job attempt failed"`) || !strings.Contains(text, receipt.ID.String()) || strings.Contains(text, "private-provider-error") || strings.Contains(text, user.String()) {
		t.Fatal("configured default job logging missing or unsafe")
	}
}
