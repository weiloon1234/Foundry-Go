package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func TestDeployJobsProcessHelper(t *testing.T) {
	if os.Getenv("FOUNDRY_CONSUMER_DEPLOY_HELPER") != "1" {
		return
	}
	marker := 0
	for marker < len(os.Args) && os.Args[marker] != "--" {
		marker++
	}
	if marker == len(os.Args) {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := RunDeployJobs(ctx, os.Args[marker+1:], cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(int(cli.Status(err)))
}

func TestDeployJobsProcessStdoutAndGlobalSettings(t *testing.T) {
	address := os.Getenv("FOUNDRY_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set FOUNDRY_TEST_REDIS_ADDR for real Redis acceptance")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	trap, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer trap.Close()
	contacts := make(chan bool, 1)
	go func() {
		conn, err := trap.Accept()
		if err == nil {
			conn.Close()
		}
		contacts <- err == nil
	}()

	dir := t.TempDir()
	connectionPath := filepath.Join(dir, "redis.connections")
	appPath := filepath.Join(dir, "application")
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	connectionJSON, err := json.Marshal(map[string]any{"default": map[string]any{
		"host": host, "port": portNumber, "tls": "disable", "user": os.Getenv("FOUNDRY_TEST_REDIS_USER"), "password": os.Getenv("FOUNDRY_TEST_REDIS_PASSWORD"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(connectionPath, connectionJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appPath, []byte("consumer-process\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := "jobs-" + rand.Text()
	configPath, envPath := filepath.Join(dir, "deploy.toml"), filepath.Join(dir, "app.env")
	configText := fmt.Sprintf(`
[app.services.namespace]
application = "must-be-overridden-by-file"
environment = %q
[app.services.redis]
default = "default"
[app.services.redis.connections.default]
host = "unreachable.invalid"
port = 1
tls = "disable"
[app.services.jobs]
default = "primary"
[app.services.jobs.connections.primary]
driver = "redis"
redis = "default"
default_queue = "delivery"
[app.log]
default = "deployment"
[app.log.channels.deployment]
stack = ["console", "remote"]
[app.log.channels.deployment.sink]
driver = "stack"
[app.log.channels.console.sink]
driver = "stdout"
[app.log.channels.remote.sink]
driver = "syslog"
[app.log.channels.remote.sink.syslog]
network = "tcp"
address = %q
`, environment, trap.Addr().String())
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	envText := fmt.Sprintf("BOOTSTRAP__APP__SERVICES__REDIS__CONNECTIONS_FILE=%s\nBOOTSTRAP__APP__SERVICES__NAMESPACE__APPLICATION_FILE=%s\n", connectionPath, appPath)
	if err := os.WriteFile(envPath, []byte(envText), 0o600); err != nil {
		t.Fatal(err)
	}
	// The parent owns only these physical addresses, including uncertain child writes.
	key, err := jobs.NewKey(keyspace.Namespace{Application: "consumer-process.jobs.primary", Environment: environment}, "delivery")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := redis.JobKeys(key)
	if err != nil {
		t.Fatal(err)
	}
	raw := driver.NewClient(&driver.Options{Addr: address, Username: os.Getenv("FOUNDRY_TEST_REDIS_USER"), Password: os.Getenv("FOUNDRY_TEST_REDIS_PASSWORD")})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := raw.Del(ctx, keys...).Err(); err != nil {
			t.Error("exact-key Redis cleanup failed")
		}
		if err := raw.Close(); err != nil {
			t.Error(err)
		}
	})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		args = append(args, "--config", configPath, "--env-file", envPath)
		command := exec.CommandContext(t.Context(), executable, append([]string{"-test.run=^TestDeployJobsProcessHelper$", "--"}, args...)...)
		command.Env = append(os.Environ(), "FOUNDRY_CONSUMER_DEPLOY_HELPER=1")
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			t.Fatalf("restricted command failed: %v\n%s", err, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("unexpected lifecycle stderr: %s", stderr.String())
		}
		return output
	}
	output := run("jobs", "stats", "--connection", "primary", "--queue", "delivery", "--format", "json")
	var stats jobs.QueueStats
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		t.Fatal("stdout contains extra output", err)
	}
	if stats != (jobs.QueueStats{}) {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if layout := raw.HGet(t.Context(), key.String()+":metadata", "layout").Val(); layout != "3" {
		t.Fatalf("settings flags did not select the expected queue: layout=%q", layout)
	}
	trap.Close()
	if <-contacts {
		t.Fatal("restricted boot contacted the application syslog sink")
	}
}
