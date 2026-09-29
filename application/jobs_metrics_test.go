package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func TestConfiguredMetricsIncludeSampledQueueDepth(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Features.Observability.Enabled = true
	c := infrastructure.DefaultJobConnectionSettings()
	c.DefaultQueue = "mail"
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": c}
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := `foundry_jobs_queue_jobs{connection="default",queue="mail",state="waiting"} 0`
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var output strings.Builder
		if err := app.Observability().WritePrometheus(&output); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), want) && strings.Contains(output.String(), "foundry_jobs_queue_sampled_timestamp_seconds") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("queue depth was not exposed")
}

// HTTP replicas and CLI commands do not sample queue depth; worker and
// scheduler processes do.
func TestQueueDepthIsSampledOnlyBesideWorkerOrSchedulerKernels(t *testing.T) {
	s := settings()
	s.Features.Observability.Enabled = true
	c := infrastructure.DefaultJobConnectionSettings()
	c.DefaultQueue = "mail"
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": c}
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	ready, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
	_, err = app.HTTPReady(ready)
	readyCancel()
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := app.Observability().WritePrometheus(&output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "foundry_jobs_queue") || strings.Contains(strings.Join(app.PendingTasks(), ","), "jobs-depth") {
		t.Fatal("an HTTP process sampled queue depth")
	}
	cancel()
	<-done
}
