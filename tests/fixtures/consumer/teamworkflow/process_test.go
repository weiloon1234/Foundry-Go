package teamworkflow

import (
	"bufio"
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// This helper runs only as an owned native process. The private database URL is
// inherited through the existing test environment, never arguments or output.
func TestWorkflowProcessChild(t *testing.T) {
	main := os.Getenv("FOUNDRY_WORKFLOW_MAIN_SCOPE")
	if main == "" {
		t.Skip("owned workflow subprocess helper")
	}
	receiver := os.Getenv("FOUNDRY_WORKFLOW_RECEIVER_SCOPE")
	settings := Defaults()
	for name, schema := range map[database.ConnectionName]string{"main": main, "receiver": receiver} {
		config := pgtest.Config(t)
		config.Schema = schema
		config.Pool.MaxOpen = 2
		config.Pool.MaxIdle = 2
		connection := infrastructure.DefaultConnectionSettings()
		connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(config)
		connection.MaxConnections = 2
		settings.Services.Database.Connections[name] = connection
	}
	settings.Services.Namespace.Application = main
	var err error
	settings, err = settings.WithDatabaseScopes(settings.Services.Database)
	if err != nil {
		t.Fatal(err)
	}
	app, err := Build(t.Context(), settings, Hooks{}, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	// Parent already ran explicit migrations in retained namespaces.
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	running := runApp(t, app)
	fmt.Println("WORKFLOW_URL " + running.client.URL)
	<-t.Context().Done()
}
func restartClient(t *testing.T, main, receiver *pgtest.Scope) *runningApp {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkflowProcessChild$", "-test.timeout=25s")
	command.Env = append(os.Environ(), "FOUNDRY_WORKFLOW_MAIN_SCOPE="+main.Schema(), "FOUNDRY_WORKFLOW_RECEIVER_SCOPE="+receiver.Schema())
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = command.Process.Kill(); _ = command.Wait(); cancel() }) }
	t.Cleanup(stop)
	lines := make(chan string, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "WORKFLOW_URL ") {
				lines <- strings.TrimPrefix(scanner.Text(), "WORKFLOW_URL ")
			}
		}
	}()
	var address string
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("workflow child failed before readiness")
		}
		address = line
	case <-ctx.Done():
		t.Fatal("workflow child did not become ready")
	}
	return &runningApp{client: httptest.Connect(t, address), stop: stop}
}
func TestWorkflowOutcomeReplaysInANewProcess(t *testing.T) {
	main, receiver := pgtest.Isolate(t), pgtest.Isolate(t)
	original := startApp(t, main, receiver, Hooks{})
	path, key := "/teams/1/projects/shared/submissions", "process-workflow-key-0001"
	first := send(t, original, "POST", path, "alice", key, `{"name":"Restart"}`, 202)
	db, _ := original.app.Resources().Database()
	waitFor(t, func() bool {
		return scalar(t, db, `SELECT count(*) FROM foundry_outbox WHERE publish_state='published'`) == 1
	})
	original.stop()
	fresh := restartClient(t, main, receiver)
	replay := send(t, fresh, "POST", path, "alice", key, `{"name":" Restart "}`, 202)
	if string(first.Bytes()) != string(replay.Bytes()) {
		t.Fatal("new process changed the committed action envelope")
	}
	assertions, receiving := main.Open(t), receiver.Open(t)
	if scalar(t, assertions, `SELECT count(*) FROM workflow_submissions`) != 1 || scalar(t, assertions, `SELECT count(*) FROM foundry_outbox`) != 1 || scalar(t, receiving, `SELECT total FROM workflow_delivery_totals WHERE name='submissions'`) != 1 {
		t.Fatal("new process duplicated a committed effect")
	}
	fresh.stop()
}
