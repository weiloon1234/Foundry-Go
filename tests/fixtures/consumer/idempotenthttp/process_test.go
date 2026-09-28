package idempotenthttp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// The parent terminates only the test binary it owns. The PostgreSQL server and
// retained schemas are never stopped, reset or cleaned up.
func TestIdempotencyProcessChild(t *testing.T) {
	mode := os.Getenv("FOUNDRY_IDEMPOTENCY_CHILD")
	if mode == "" {
		t.Skip("owned subprocess helper")
	}
	phase, schema := os.Getenv("FOUNDRY_IDEMPOTENCY_PHASE"), os.Getenv("FOUNDRY_IDEMPOTENCY_SCHEMA")
	config := pgtest.Config(t)
	config.Schema = schema
	config.Pool.MaxOpen = 2
	config.Pool.MaxIdle = 2
	settings := Defaults()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(config)
	connection.MaxConnections = 2
	settings.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	settings.Services.Namespace.Application = schema
	settings, err := settings.WithDatabaseScopes(settings.Services.Database)
	if err != nil {
		t.Fatal(err)
	}
	hooks := Hooks{}
	if mode == "crash" && phase == "before" {
		hooks.InTransaction = func(ctx context.Context, _ *database.Tx, _ BoundRequest) error {
			fmt.Println("IDEMPOTENCY_BEFORE")
			<-ctx.Done()
			return ctx.Err()
		}
	}
	if mode == "crash" && phase == "after" {
		hooks.AfterCommit = func(ctx context.Context, _ Receipt) error {
			fmt.Println("IDEMPOTENCY_AFTER")
			<-ctx.Done()
			return ctx.Err()
		}
	}
	app, err := Build(t.Context(), settings, hooks, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	running := runApp(t, app)
	fmt.Println("IDEMPOTENCY_URL " + running.url)
	<-t.Context().Done()
}

type childProcess struct {
	url    string
	events <-chan string
	stop   func()
}

func startChild(t *testing.T, schema, phase, mode string) *childProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIdempotencyProcessChild$", "-test.timeout=40s")
	command.Env = append(os.Environ(), "FOUNDRY_IDEMPOTENCY_CHILD="+mode, "FOUNDRY_IDEMPOTENCY_PHASE="+phase, "FOUNDRY_IDEMPOTENCY_SCHEMA="+schema)
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
	events := make(chan string, 8)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "IDEMPOTENCY_") {
				events <- scanner.Text()
			}
		}
	}()
	var once sync.Once
	child := &childProcess{events: events}
	child.stop = func() { once.Do(func() { _ = command.Process.Kill(); _ = command.Wait(); cancel() }) }
	t.Cleanup(child.stop)
	select {
	case line, ok := <-events:
		if !ok || !strings.HasPrefix(line, "IDEMPOTENCY_URL ") {
			t.Fatal("owned child did not start its HTTP kernel")
		}
		child.url = strings.TrimPrefix(line, "IDEMPOTENCY_URL ")
	case <-ctx.Done():
		t.Fatal("owned child startup timed out")
	}
	return child
}
func TestHTTPProcessTerminationBeforeAndAfterCommit(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			scope := pgtest.Isolate(t)
			prepared := startApp(t, scope, Hooks{}, nil)
			prepared.stop()
			assertions := scope.Open(t)
			child := startChild(t, scope.Schema(), phase, "crash")
			client := &runningApp{url: child.url, client: &http.Client{Timeout: 10 * time.Second}}
			key, body := "process-key-"+phase+"-0001", `{"name":"`+phase+`"}`
			failed := make(chan wireResponse, 1)
			go func() { failed <- client.send(t.Context(), "/workspaces/1/orders", "alice", key, body) }()
			select {
			case line := <-child.events:
				if line != "IDEMPOTENCY_"+strings.ToUpper(phase) {
					t.Fatal("wrong process boundary")
				}
			case <-time.After(8 * time.Second):
				t.Fatal("process did not reach transaction boundary")
			}
			before := scalar(t, assertions, `SELECT count(*) FROM idem_orders`)
			want := int64(0)
			if phase == "after" {
				want = 1
			}
			if before != want {
				t.Fatal("commit boundary did not match persisted data")
			}
			child.stop()
			if response := <-failed; response.err == nil && response.status < 400 {
				t.Fatal("terminated process delivered an unexpected success")
			}
			fresh := startChild(t, scope.Schema(), phase, "replay")
			next := &runningApp{url: fresh.url, client: &http.Client{Timeout: 10 * time.Second}}
			assertResponse(t, next.send(t.Context(), "/workspaces/1/orders", "alice", key, body), 201)
			if scalar(t, assertions, `SELECT count(*) FROM idem_orders`) != 1 || scalar(t, assertions, `SELECT count(*) FROM foundry_outbox`) != 1 || scalar(t, assertions, `SELECT count(*) FROM foundry_idempotency WHERE completed_at IS NOT NULL`) != 1 {
				t.Fatal("fresh process duplicated or lost a local outcome")
			}
			assertResponse(t, next.send(t.Context(), "/workspaces/1/orders", "alice", key, body), 201)
			if scalar(t, assertions, `SELECT count(*) FROM idem_orders`) != 1 {
				t.Fatal("fresh-process replay executed again")
			}
			fresh.stop()
		})
	}
}
