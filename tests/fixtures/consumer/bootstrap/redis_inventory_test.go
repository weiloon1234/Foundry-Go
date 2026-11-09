package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestRedisInventoryBeforeApplicationBuild(t *testing.T) {
	settings := application.DefaultSettings()
	settings.Services.Namespace = keyspace.Namespace{Application: "consumer", Environment: "production"}
	connection := infrastructure.DefaultRedisConnectionSettings()
	connection.Host = "unreachable.invalid"
	connection.Password = secret.New("never-in-inventory")
	settings.Services.Redis.Connections = infrastructure.RedisConnections{"default": connection}
	limits := foundation.NewKey[*ratelimit.Store]("consumer.limits")
	protection := foundation.NewKey[*lockout.Store]("consumer.protection")
	builder := application.New(settings,
		application.WithRedisStores(
			infrastructure.RedisRateLimitStore(limits, "", ratelimit.DefaultConfig(keyspace.Namespace{})),
			infrastructure.RedisLockoutStore(protection, "", lockout.DefaultConfig(keyspace.Namespace{})),
		),
		application.WithCredentials("default", credentials.ProviderFunc(func(context.Context) (credentials.Value, error) { panic("inspection retrieved credentials") })),
	).Register(foundation.Module{Name: "must-not-run", OnRegister: func(*foundation.Registrar) error { panic("inspection ran an application provider") }})
	// doc-example:begin redis-inventory
	inventory, err := builder.RedisKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 1 || len(inventory[0].Keys) != 2 {
		t.Fatal(inventory)
	}
	// doc-example:end redis-inventory

	command, err := application.RedisKeysCommand("redis.keys", builder)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"redis.keys"}, {"redis.keys", "--json"}} {
		var output strings.Builder
		invocation, err := registry.Parse(args, &output)
		if err != nil {
			t.Fatal(err)
		}
		if err := invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}); err != nil {
			t.Fatal(err)
		}
		wire := output.String()
		if strings.Contains(wire, "never-in-inventory") || strings.Contains(wire, "unreachable") {
			t.Fatal("inspection exposed private configuration")
		}
		for _, prefix := range []string{"foundry:ratelimit:v1:consumer:production:", "foundry:lockout:v1:consumer:production:"} {
			if !strings.Contains(wire, prefix) {
				t.Fatal("missing prefix", prefix)
			}
		}
		if len(args) == 2 {
			var decoded []infrastructure.RedisConnectionInventory
			if err := json.Unmarshal([]byte(wire), &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded) != 1 || len(decoded[0].Keys) != 2 {
				t.Fatal(decoded)
			}
		}
	}
}

func TestConsumerJobQueuesRestrictedCLIWithoutHandlerConstruction(t *testing.T) {
	address := os.Getenv("FOUNDRY_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set FOUNDRY_TEST_REDIS_ADDR for real Redis acceptance")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	envName := "consumer-cutover-" + hex.EncodeToString(random[:])

	secretFile := filepath.Join(t.TempDir(), "redis.password")
	if err := os.WriteFile(secretFile, []byte(os.Getenv("FOUNDRY_TEST_REDIS_PASSWORD")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOOTSTRAP_APP_SERVICES_REDIS_CONNECTIONS_DEFAULT_PASSWORD_FILE", secretFile)

	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	settings := loaded.App
	settings.Services.Namespace = keyspace.Namespace{Application: "consumer", Environment: envName}

	redisConn := infrastructure.DefaultRedisConnectionSettings()
	redisConn.Host = host
	redisConn.Port = uint16(port)
	redisConn.TLS = redis.DisableTLS
	redisConn.User = os.Getenv("FOUNDRY_TEST_REDIS_USER")
	redisConn.Password = settings.Services.Redis.Connections["default"].Password
	settings.Services.Redis.Default = "default"
	settings.Services.Redis.Connections = infrastructure.RedisConnections{"default": redisConn}

	deliveryConn := infrastructure.DefaultJobConnectionSettings()
	deliveryConn.Driver = infrastructure.RedisJobs
	deliveryConn.Redis = "default"
	deliveryConn.DefaultQueue = "delivery"
	deliveryConn.Dispatch.Namespace = keyspace.Namespace{Application: "consumer", Environment: envName}

	auditConn := infrastructure.DefaultJobConnectionSettings()
	auditConn.Driver = infrastructure.RedisJobs
	auditConn.Redis = "default"
	auditConn.DefaultQueue = "audit"
	auditConn.Dispatch.Namespace = keyspace.Namespace{Application: "consumer-audit", Environment: envName}

	settings.Services.Jobs = infrastructure.JobsSettings{
		Default: "delivery",
		Connections: infrastructure.JobConnections{
			"delivery": deliveryConn,
			"audit":    auditConn,
		},
	}
	settings.Worker.Enabled = true
	settings.Worker.Connection = "delivery"
	settings.Worker.Config.Queues = []jobs.Subscription{
		{Queue: "delivery", Weight: 2},
		{Queue: "priority", Weight: 1},
	}

	auditJob := jobs.Define[Profile]("bootstrap.audit", 1, jobs.DefaultPolicy("audit-events"))
	builder := application.New(
		settings,
		application.WithCredentials("default", credentials.ProviderFunc(func(context.Context) (credentials.Value, error) {
			panic("job queue inspection retrieved cloud credentials")
		})),
	).Jobs(
		application.Job(ProfileJob, func(application.Services) (jobs.Handler[Profile], error) {
			panic("job queue inspection constructed ProfileJob handler")
		}),
		application.Job(auditJob, func(application.Services) (jobs.Handler[Profile], error) {
			panic("job queue inspection constructed auditJob handler")
		}).On("audit"),
	)

	declarations, err := application.DeployJobCommands(builder)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declarations...)
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	invocation, err := registry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := invocation.Run(ctx, nil, cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}); err != nil {
		t.Fatalf("deploy:jobs-inspect failed: %v\n%s", err, output.String())
	}

	var report application.JobQueueReport
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != 1 || report.SupportedLayout != 3 || report.Coverage != application.JobQueueCoverageDeclared || len(report.Blockers) != 0 {
		t.Fatalf("unexpected report envelope: %+v", report)
	}
	if len(report.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %+v", report.Connections)
	}
	if report.Connections[0].Connection != "audit" || len(report.Connections[0].Queues) != 2 ||
		report.Connections[0].Queues[0].Queue != "audit" || report.Connections[0].Queues[1].Queue != "audit-events" {
		t.Fatalf("unexpected audit connection queues: %+v", report.Connections[0])
	}
	if report.Connections[1].Connection != "delivery" || len(report.Connections[1].Queues) != 3 ||
		report.Connections[1].Queues[0].Queue != "delivery" || report.Connections[1].Queues[1].Queue != "priority" || report.Connections[1].Queues[2].Queue != "profiles" {
		t.Fatalf("unexpected delivery connection queues: %+v", report.Connections[1])
	}
	for _, conn := range report.Connections {
		for _, q := range conn.Queues {
			if q.Layout != redis.JobQueueLayoutAbsent || !q.Eligible {
				t.Fatalf("expected absent eligible queue, got %+v", q)
			}
		}
	}
}
