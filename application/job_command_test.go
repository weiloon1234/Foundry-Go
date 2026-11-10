package application

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func TestDeployJobCommandsRunWithoutApplicationProviders(t *testing.T) {
	settings := DefaultSettings()
	settings.Services.Jobs.Connections = infrastructure.JobConnections{"default": infrastructure.DefaultJobConnectionSettings()}
	definition := jobs.Define[struct{}]("restricted.work", 1, jobs.DefaultPolicy("default"))
	builder := New(settings).Register(foundation.Module{
		Name:       "must-not-run",
		OnRegister: func(*foundation.Registrar) error { panic("restricted command registered application provider") },
	}).Jobs(Job(definition, func(Services) (jobs.Handler[struct{}], error) {
		panic("restricted command constructed business handler")
	}))
	declarations, err := DeployJobCommands(builder)
	if err != nil || len(declarations) != 1 {
		t.Fatal("expected one jobs declaration", err)
	}
	registry, err := cli.New(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	invocation, err := registry.Parse([]string{"jobs", "stats", "--format", "json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.Run(t.Context(), nil, cli.Streams{In: bytes.NewReader(nil), Out: &output, Err: io.Discard}); err != nil {
		t.Fatal(err)
	}
	var stats jobs.QueueStats
	if err := json.Unmarshal(output.Bytes(), &stats); err != nil || stats != (jobs.QueueStats{}) {
		t.Fatal("unexpected queue statistics", stats, err)
	}
}
