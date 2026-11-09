package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type testOpaquePlugin struct{}

func (testOpaquePlugin) Manifest() pluginmanifest.Manifest               { return pluginmanifest.Manifest{} }
func (testOpaquePlugin) Register(*foundation.Registrar) error            { return nil }
func (testOpaquePlugin) Boot(context.Context, *foundation.Runtime) error { return nil }
func (testOpaquePlugin) Shutdown(context.Context, *foundation.Runtime) error {
	return nil
}

type inventoryTestPayload struct {
	Message string `json:"message"`
}

func redisAddrFromEnv(t *testing.T) (string, uint16) {
	t.Helper()
	address := os.Getenv("FOUNDRY_TEST_REDIS_ADDR")
	if address == "" {
		if os.Getenv("FOUNDRY_TEST_REDIS_REQUIRED") == "1" {
			t.Fatal("FOUNDRY_TEST_REDIS_ADDR is required")
		}
		t.Skip("set FOUNDRY_TEST_REDIS_ADDR for real Redis acceptance")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("invalid FOUNDRY_TEST_REDIS_ADDR: %v", err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatalf("invalid FOUNDRY_TEST_REDIS_ADDR port: %v", err)
	}
	return host, uint16(port)
}

func redisConfigForTest(host string, port uint16) redis.Config {
	cfg := redis.DefaultConfig()
	cfg.Host = host
	cfg.Port = port
	cfg.TLS = redis.DisableTLS
	cfg.User = os.Getenv("FOUNDRY_TEST_REDIS_USER")
	cfg.Password = secret.New(os.Getenv("FOUNDRY_TEST_REDIS_PASSWORD"))
	return cfg
}

func randomEnvironment(t *testing.T) string {
	t.Helper()
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatal(err)
	}
	return "cutover-" + hex.EncodeToString(buf[:])
}

func TestJobQueuesEmptyWhenNoRedisQueuesConfigured(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*Settings)
	}{
		{
			name: "memory_and_sync_only",
			configure: func(s *Settings) {
				syncConn := infrastructure.DefaultJobConnectionSettings()
				syncConn.Driver = infrastructure.SyncJobs
				s.Services.Jobs.Connections = infrastructure.JobConnections{
					"default": infrastructure.DefaultJobConnectionSettings(),
					"inline":  syncConn,
				}
			},
		},
		{
			name: "no_job_connections",
			configure: func(s *Settings) {
				s.Services.Jobs = infrastructure.JobsSettings{}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSettings()
			s.Services.Namespace = keyspace.Namespace{Application: "foundry-app", Environment: "testing"}
			tc.configure(&s)
			builder := New(s)

			report, err := builder.JobQueues()
			if err != nil {
				t.Fatalf("expected nil error for no-Redis instance, got %v", err)
			}
			if report.Schema != 1 || report.SupportedLayout != 3 || report.Coverage != JobQueueCoverageDeclared || len(report.Connections) != 0 || len(report.Blockers) != 0 {
				t.Fatalf("unexpected report: %+v", report)
			}

			cmd, err := JobQueuesCommand("deploy:jobs-inspect", builder)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := cli.New(cmd)
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := registry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			var out, errBuf bytes.Buffer
			runErr := invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf})
			if cli.Status(runErr) != cli.Success {
				t.Fatalf("expected exit 0, got status=%d err=%v", cli.Status(runErr), runErr)
			}
			var raw map[string]any
			if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if len(raw) != 5 {
				t.Fatalf("expected exactly 5 top-level JSON fields, got %v", raw)
			}
			var decoded JobQueueReport
			dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Connections == nil || len(decoded.Connections) != 0 || decoded.Blockers == nil || len(decoded.Blockers) != 0 {
				t.Fatalf("expected non-nil empty connections and blockers arrays, got %+v", decoded)
			}
		})
	}
}

func TestJobQueuesFailClosedCoverageAndBounds(t *testing.T) {
	baseSettings := func() Settings {
		s := DefaultSettings()
		s.Services.Namespace = keyspace.Namespace{Application: "foundry-app", Environment: "production"}
		rCfg := infrastructure.DefaultRedisConnectionSettings()
		rCfg.Host = "127.0.0.1"
		rCfg.Port = 6379
		rCfg.TLS = redis.DisableTLS
		s.Services.Redis = infrastructure.RedisSettings{
			Default:        "primary",
			MaxConnections: 1024,
			Connections:    infrastructure.RedisConnections{"primary": rCfg},
		}
		jCfg := infrastructure.DefaultJobConnectionSettings()
		jCfg.Driver = infrastructure.RedisJobs
		jCfg.Redis = "primary"
		s.Services.Jobs.Connections = infrastructure.JobConnections{"default": jCfg}
		return s
	}

	t.Run("opaque_provider", func(t *testing.T) {
		builder := New(baseSettings()).Register(foundation.Module{Name: "custom.opaque.provider"})
		report, err := builder.JobQueues()
		if err == nil {
			t.Fatal("expected error for opaque provider")
		}
		if len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerOpaqueProvider || !report.Blockers[0].Connection.IsNull() || !report.Blockers[0].Queue.IsNull() {
			t.Fatalf("unexpected blockers: %+v", report.Blockers)
		}
	})

	t.Run("opaque_plugin", func(t *testing.T) {
		builder := New(baseSettings()).RegisterPlugin(testOpaquePlugin{})
		report, err := builder.JobQueues()
		if err == nil {
			t.Fatal("expected error for opaque plugin")
		}
		if len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerOpaqueProvider || !report.Blockers[0].Connection.IsNull() || !report.Blockers[0].Queue.IsNull() {
			t.Fatalf("unexpected blockers: %+v", report.Blockers)
		}
	})

	t.Run("invalid_job_declaration_unconfigured_connection", func(t *testing.T) {
		def := jobs.Define[inventoryTestPayload]("orders.send", 1, jobs.DefaultPolicy("orders"))
		builder := New(baseSettings()).Jobs(Job(def, func(Services) (jobs.Handler[inventoryTestPayload], error) {
			t.Fatal("handler constructor must not run during inventory")
			return nil, nil
		}).On("missing"))

		cmd, err := JobQueuesCommand("deploy:jobs-inspect", builder)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := cli.New(cmd)
		if err != nil {
			t.Fatal(err)
		}
		invocation, err := registry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		var out, errBuf bytes.Buffer
		runErr := invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf})
		if cli.Status(runErr) != cli.Failure {
			t.Fatalf("expected exit 1, got %d (%v)", cli.Status(runErr), runErr)
		}
		var report JobQueueReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerInvalidConfiguration || !report.Blockers[0].Connection.IsNull() || !report.Blockers[0].Queue.IsNull() {
			t.Fatalf("expected global invalid_configuration blocker, got %+v", report.Blockers)
		}
	})

	t.Run("duplicate_job_declaration", func(t *testing.T) {
		def := jobs.Define[inventoryTestPayload]("orders.send", 1, jobs.DefaultPolicy("orders"))
		decl := Job(def, func(Services) (jobs.Handler[inventoryTestPayload], error) {
			return func(context.Context, inventoryTestPayload) error { return nil }, nil
		})
		builder := New(baseSettings()).Jobs(decl, decl)
		report, err := builder.JobQueues()
		if err == nil || len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerInvalidConfiguration {
			t.Fatalf("expected invalid_configuration blocker on duplicate job, got %+v err=%v", report, err)
		}
	})

	t.Run("nil_job_constructor", func(t *testing.T) {
		def := jobs.Define[inventoryTestPayload]("orders.send", 1, jobs.DefaultPolicy("orders"))
		builder := New(baseSettings()).Jobs(Job[inventoryTestPayload](def, nil))
		report, err := builder.JobQueues()
		if err == nil || len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerInvalidConfiguration {
			t.Fatalf("expected invalid_configuration blocker on nil job constructor, got %+v err=%v", report, err)
		}
	})

	t.Run("inventory_limit_connections", func(t *testing.T) {
		s := baseSettings()
		s.Services.Jobs.Connections = make(infrastructure.JobConnections, MaxJobQueueConnections+1)
		for i := range MaxJobQueueConnections + 1 {
			name := jobs.ConnectionName(fmt.Sprintf("conn-%02d", i))
			jCfg := infrastructure.DefaultJobConnectionSettings()
			jCfg.Driver = infrastructure.RedisJobs
			jCfg.Redis = "primary"
			s.Services.Jobs.Connections[name] = jCfg
		}
		s.Services.Jobs.Default = "conn-00"
		// namedservice.MaxEntries is 64; verify that exceeding 64 connections or >4096 queues fails closed.
		// Test > MaxJobQueueConnections directly via WriteJobQueues / encodeJobQueueReport and > 64 connections.
		report, err := New(s).JobQueues()
		if err == nil || len(report.Blockers) != 1 {
			t.Fatalf("expected blocker for excessive connections, got %+v err=%v", report, err)
		}
	})

	t.Run("inventory_limit_queues", func(t *testing.T) {
		s := baseSettings()
		// Register 4096 jobs on unique queues plus the connection's default queue = 4097 total queues (> MaxJobQueues = 4096).
		decls := make([]JobDeclaration, 0, MaxJobQueues)
		for i := range MaxJobQueues {
			q := jobs.Queue(fmt.Sprintf("queue-%04d", i))
			name := jobs.Name(fmt.Sprintf("domain.job-%04d", i))
			def := jobs.Define[inventoryTestPayload](name, 1, jobs.DefaultPolicy(q))
			decls = append(decls, Job(def, func(Services) (jobs.Handler[inventoryTestPayload], error) {
				return func(context.Context, inventoryTestPayload) error { return nil }, nil
			}))
		}
		builder := New(s).Jobs(decls...)
		report, err := builder.JobQueues()
		if err == nil {
			t.Fatal("expected error when total queues exceed MaxJobQueues")
		}
		if len(report.Connections) != 0 || len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerInventoryLimit {
			t.Fatalf("expected fail-closed inventory_limit blocker, got %+v", report)
		}
	})
}

func TestJobQueuesStorageAndPolicyIdentityAndAliases(t *testing.T) {
	expectedDigest := sha256.Sum256([]byte(`["localhost",6379,2,"app-user",false]`))
	expectedStorageID := hex.EncodeToString(expectedDigest[:])

	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "::1", "[::1]"} {
		cfg := redis.DefaultConfig()
		cfg.Host = host
		cfg.Port = 6379
		cfg.Database = 2
		cfg.User = "app-user"
		cfg.Password = secret.New("super-secret-password-never-in-digest")
		cfg.TLS = redis.DisableTLS
		got, err := cfg.StorageID()
		if err != nil {
			t.Fatalf("host %q: %v", host, err)
		}
		if got != expectedStorageID {
			t.Fatalf("host %q: got storage_id %s, want %s", host, got, expectedStorageID)
		}
	}

	s := DefaultSettings()
	s.Services.Namespace = keyspace.Namespace{Application: "foundry-app", Environment: "production"}
	rCfg := infrastructure.DefaultRedisConnectionSettings()
	rCfg.Host = "127.0.0.1"
	rCfg.Port = 6379
	rCfg.Database = 2
	rCfg.User = "app-user"
	rCfg.Password = secret.New("private-redis-secret")
	rCfg.TLS = redis.DisableTLS
	s.Services.Redis = infrastructure.RedisSettings{
		Default:        "primary",
		MaxConnections: 1024,
		Connections:    infrastructure.RedisConnections{"primary": rCfg},
	}

	sharedNamespace := keyspace.Namespace{Application: "foundry-app", Environment: "production"}
	connA := infrastructure.DefaultJobConnectionSettings()
	connA.Driver = infrastructure.RedisJobs
	connA.Redis = "primary"
	connA.DefaultQueue = "shared"
	connA.Dispatch.Namespace = sharedNamespace

	connB := connA // Exact physical alias of connA under a second logical connection name.
	s.Services.Jobs = infrastructure.JobsSettings{
		Default: "default",
		Connections: infrastructure.JobConnections{
			"default": connA,
			"alias":   connB,
		},
	}

	report, err := New(s).JobQueues()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(report.Connections))
	}
	first, second := report.Connections[0], report.Connections[1]
	if first.Connection != "alias" || second.Connection != "default" {
		t.Fatalf("expected lexical connection order [alias, default], got [%s, %s]", first.Connection, second.Connection)
	}
	if first.StorageID != expectedStorageID || second.StorageID != expectedStorageID {
		t.Fatalf("unexpected storage_id: %s vs %s", first.StorageID, second.StorageID)
	}
	if first.PolicyID != second.PolicyID || first.Prefix != second.Prefix || first.Namespace != second.Namespace {
		t.Fatalf("alias connections differed in policy_id/prefix/namespace: %+v vs %+v", first, second)
	}
	if len(first.Queues) != 1 || len(second.Queues) != 1 || first.Queues[0].Base != second.Queues[0].Base || !slices.Equal(first.Queues[0].Keys, second.Queues[0].Keys) {
		t.Fatalf("alias queues differed in base or keys: %+v vs %+v", first.Queues, second.Queues)
	}
	if len(first.Queues[0].Keys) != 11 || !slices.IsSorted(first.Queues[0].Keys) {
		t.Fatalf("expected 11 sorted physical keys, got %v", first.Queues[0].Keys)
	}
}

func TestJobQueuesLiveLayoutsBlockersAndCutoverMigration(t *testing.T) {
	host, port := redisAddrFromEnv(t)
	env := randomEnvironment(t)
	nsPrimary := keyspace.Namespace{Application: "cutover-m1", Environment: env}
	nsSecondary := keyspace.Namespace{Application: "cutover-m1-sec", Environment: env}

	rCfg := redisConfigForTest(host, port)
	raw := driver.NewClient(&driver.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(int(port))),
		Username: rCfg.User,
		Password: rCfg.Password.Reveal(),
		DB:       rCfg.Database,
	})
	t.Cleanup(func() { _ = raw.Close() })

	var trackedKeys []string
	trackQueue := func(ns keyspace.Namespace, q jobs.Queue) jobs.Key {
		key, err := jobs.NewKey(ns, q)
		if err != nil {
			t.Fatal(err)
		}
		phys, err := redis.JobKeys(key)
		if err != nil {
			t.Fatal(err)
		}
		trackedKeys = append(trackedKeys, phys...)
		return key
	}
	t.Cleanup(func() {
		if len(trackedKeys) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = raw.Del(ctx, trackedKeys...).Err()
		}
	})

	client, err := redis.Open(t.Context(), rCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Close(ctx)
	})

	queueCfg := jobs.DefaultQueueConfig()
	backend, err := redis.NewJobBackend(client, queueCfg)
	if err != nil {
		t.Fatal(err)
	}

	// Primary queues:
	// - "default": layout 2 (seeded by enqueuing a job and setting metadata layout='2')
	// - "emails": layout 3 (seeded by enqueuing a job)
	// - "reports": absent (declared via worker subscription)
	keyDefault := trackQueue(nsPrimary, "default")
	keyEmails := trackQueue(nsPrimary, "emails")
	keyReports := trackQueue(nsPrimary, "reports")

	defDefault := jobs.Define[inventoryTestPayload]("cutover.default", 1, jobs.DefaultPolicy("default"))
	defEmails := jobs.Define[inventoryTestPayload]("cutover.emails", 1, jobs.DefaultPolicy("emails"))

	pendingDefault, err := defDefault.Capture(t.Context(), inventoryTestPayload{Message: "layout-2"}, jobs.Options[inventoryTestPayload]{})
	if err != nil {
		t.Fatal(err)
	}
	if inserted, err := backend.JobEnqueue(t.Context(), keyDefault, pendingDefault.Envelope()); err != nil || !inserted {
		t.Fatalf("seed default queue: inserted=%v err=%v", inserted, err)
	}
	if err := raw.HSet(t.Context(), keyDefault.String()+":metadata", "layout", "2").Err(); err != nil {
		t.Fatal(err)
	}

	pendingEmails, err := defEmails.Capture(t.Context(), inventoryTestPayload{Message: "layout-3"}, jobs.Options[inventoryTestPayload]{})
	if err != nil {
		t.Fatal(err)
	}
	if inserted, err := backend.JobEnqueue(t.Context(), keyEmails, pendingEmails.Envelope()); err != nil || !inserted {
		t.Fatalf("seed emails queue: inserted=%v err=%v", inserted, err)
	}

	// Secondary connection queues for blocker states:
	// - "corrupt": orphan key without metadata -> invalid_queue_state
	// - "future": layout="99" -> unsupported_layout
	// - "legacy": layout="1" -> layout_1_requires_upgrade
	// - "mismatch": different config JSON -> queue_policy_mismatch
	keyCorrupt := trackQueue(nsSecondary, "corrupt")
	keyFuture := trackQueue(nsSecondary, "future")
	keyLegacy := trackQueue(nsSecondary, "legacy")
	keyMismatch := trackQueue(nsSecondary, "mismatch")

	configBytes, err := json.Marshal(queueCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Set(t.Context(), keyCorrupt.String()+":ready", "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(t.Context(), keyFuture.String()+":metadata", "config", string(configBytes), "layout", "99", "time", 1000).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(t.Context(), keyLegacy.String()+":metadata", "config", string(configBytes), "layout", "1", "time", 1000).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(t.Context(), keyMismatch.String()+":metadata", "config", `{"MaxEntries":7}`, "layout", "3", "time", 1000).Err(); err != nil {
		t.Fatal(err)
	}

	var handlerConstructed atomic.Bool
	buildCandidate := func(includeSecondary bool) *Builder {
		s := DefaultSettings()
		s.Services.Namespace = nsPrimary
		s.Services.Redis = infrastructure.RedisSettings{
			Default:        "primary",
			MaxConnections: 1024,
			Connections:    infrastructure.RedisConnections{"primary": infrastructure.RedisSettingsFromConfig(rCfg)},
		}
		primaryConn := infrastructure.DefaultJobConnectionSettings()
		primaryConn.Driver = infrastructure.RedisJobs
		primaryConn.Redis = "primary"
		primaryConn.DefaultQueue = "default"
		primaryConn.Dispatch.Namespace = nsPrimary
		primaryConn.Queue = queueCfg

		s.Services.Jobs = infrastructure.JobsSettings{
			Default: "default",
			Connections: infrastructure.JobConnections{
				"default": primaryConn,
			},
		}
		if includeSecondary {
			secConn := infrastructure.DefaultJobConnectionSettings()
			secConn.Driver = infrastructure.RedisJobs
			secConn.Redis = "primary"
			secConn.DefaultQueue = "corrupt"
			secConn.Dispatch.Namespace = nsSecondary
			secConn.Queue = queueCfg
			s.Services.Jobs.Connections["secondary"] = secConn
		}
		s.Worker.Enabled = true
		s.Worker.Connection = "default"
		s.Worker.Config.Queues = []jobs.Subscription{
			{Queue: "default", Weight: 1},
			{Queue: "reports", Weight: 1},
		}

		b := New(s).Jobs(
			Job(defDefault, func(Services) (jobs.Handler[inventoryTestPayload], error) {
				handlerConstructed.Store(true)
				return func(context.Context, inventoryTestPayload) error { return nil }, nil
			}),
			Job(defEmails, func(Services) (jobs.Handler[inventoryTestPayload], error) {
				handlerConstructed.Store(true)
				return func(context.Context, inventoryTestPayload) error { return nil }, nil
			}),
		)
		if includeSecondary {
			for _, q := range []jobs.Queue{"future", "legacy", "mismatch"} {
				def := jobs.Define[inventoryTestPayload](jobs.Name("sec."+string(q)), 1, jobs.DefaultPolicy(q))
				b.Jobs(Job(def, func(Services) (jobs.Handler[inventoryTestPayload], error) {
					handlerConstructed.Store(true)
					return func(context.Context, inventoryTestPayload) error { return nil }, nil
				}).On("secondary"))
			}
		}
		return b
	}

	// Phase 1: Candidate with secondary blocker queues fails closed with exit 1 and exact blockers.
	blockedBuilder := buildCandidate(true)
	blockedCmds, err := DeployJobCommands(blockedBuilder)
	if err != nil {
		t.Fatal(err)
	}
	blockedRegistry, err := cli.New(blockedCmds...)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := blockedRegistry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runErr := inv.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}})
	if cli.Status(runErr) != cli.Failure {
		t.Fatalf("expected exit 1 when blockers exist, got %d (%v)", cli.Status(runErr), runErr)
	}
	var blockedReport JobQueueReport
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&blockedReport); err != nil {
		t.Fatal(err)
	}
	if len(blockedReport.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(blockedReport.Connections))
	}
	expectedBlockers := []JobQueueBlocker{
		{Code: JobQueueBlockerInvalidQueueState, Connection: value.Of(jobs.ConnectionName("secondary")), Queue: value.Of(jobs.Queue("corrupt"))},
		{Code: JobQueueBlockerUnsupportedLayout, Connection: value.Of(jobs.ConnectionName("secondary")), Queue: value.Of(jobs.Queue("future"))},
		{Code: JobQueueBlockerLayout1RequiresUpgrade, Connection: value.Of(jobs.ConnectionName("secondary")), Queue: value.Of(jobs.Queue("legacy"))},
		{Code: JobQueueBlockerQueuePolicyMismatch, Connection: value.Of(jobs.ConnectionName("secondary")), Queue: value.Of(jobs.Queue("mismatch"))},
	}
	if !slices.Equal(blockedReport.Blockers, expectedBlockers) {
		t.Fatalf("unexpected blockers:\ngot:  %+v\nwant: %+v", blockedReport.Blockers, expectedBlockers)
	}

	// Phase 2: Candidate with only eligible queues (absent, 2, 3) exits 0, mutates nothing, and migrates layout 2 -> 3 via restricted JobCommand.
	cleanBuilder := buildCandidate(false)
	cleanCmds, err := DeployJobCommands(cleanBuilder)
	if err != nil {
		t.Fatal(err)
	}
	cleanRegistry, err := cli.New(cleanCmds...)
	if err != nil {
		t.Fatal(err)
	}
	inv, err = cleanRegistry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	runErr = inv.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}})
	if cli.Status(runErr) != cli.Success {
		t.Fatalf("expected exit 0 for eligible queues, got %d (%v)\n%s", cli.Status(runErr), runErr, out.String())
	}
	var cleanReport JobQueueReport
	if err := json.Unmarshal(out.Bytes(), &cleanReport); err != nil {
		t.Fatal(err)
	}
	if len(cleanReport.Blockers) != 0 || len(cleanReport.Connections) != 1 || len(cleanReport.Connections[0].Queues) != 3 {
		t.Fatalf("unexpected clean report: %+v", cleanReport)
	}
	queues := cleanReport.Connections[0].Queues
	if queues[0].Queue != "default" || queues[0].Layout != redis.JobQueueLayout2 || !queues[0].Eligible {
		t.Fatalf("unexpected default queue state: %+v", queues[0])
	}
	if queues[1].Queue != "emails" || queues[1].Layout != redis.JobQueueLayout3 || !queues[1].Eligible {
		t.Fatalf("unexpected emails queue state: %+v", queues[1])
	}
	if queues[2].Queue != "reports" || queues[2].Layout != redis.JobQueueLayoutAbsent || !queues[2].Eligible {
		t.Fatalf("unexpected reports queue state: %+v", queues[2])
	}
	// Verify absent queue was not created in Redis and layout-2 queue was not mutated by inspection.
	if exists, err := raw.Exists(t.Context(), keyReports.String()+":metadata").Result(); err != nil || exists != 0 {
		t.Fatalf("absent queue metadata was created by inspection: exists=%d err=%v", exists, err)
	}
	if layoutVal, err := raw.HGet(t.Context(), keyDefault.String()+":metadata", "layout").Result(); err != nil || layoutVal != "2" {
		t.Fatalf("layout-2 queue was mutated by inspection: layout=%q err=%v", layoutVal, err)
	}

	// Phase 3: Run jobs migrate-layout through the restricted CLI registry, then re-inspect to confirm layout 3.
	migrateInv, err := cleanRegistry.Parse([]string{"jobs", "migrate-layout", "--connection", "default", "--queue", "default", "--confirm", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := migrateInv.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"migrated":true`) {
		t.Fatalf("expected migrate-layout migrated=true, got %s", out.String())
	}

	reInv, err := cleanRegistry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := reInv.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &cleanReport); err != nil {
		t.Fatal(err)
	}
	if cleanReport.Connections[0].Queues[0].Layout != redis.JobQueueLayout3 || !cleanReport.Connections[0].Queues[0].Eligible {
		t.Fatalf("expected default queue at layout 3 after migration, got %+v", cleanReport.Connections[0].Queues[0])
	}
	if handlerConstructed.Load() {
		t.Fatal("business job handler constructor was invoked during inspection or restricted layout migration")
	}
}

func TestJobQueuesRedisUnavailableAndUsageExitCodes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := uint16(listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()

	s := DefaultSettings()
	s.Services.Namespace = keyspace.Namespace{Application: "foundry-app", Environment: "production"}
	rCfg := infrastructure.DefaultRedisConnectionSettings()
	rCfg.Host = "127.0.0.1"
	rCfg.Port = closedPort
	rCfg.TLS = redis.DisableTLS
	rCfg.ConnectTimeout = 100 * time.Millisecond
	rCfg.OperationTimeout = 100 * time.Millisecond
	s.Services.Redis = infrastructure.RedisSettings{
		Default:        "primary",
		MaxConnections: 1024,
		Connections:    infrastructure.RedisConnections{"primary": rCfg},
	}
	jCfg := infrastructure.DefaultJobConnectionSettings()
	jCfg.Driver = infrastructure.RedisJobs
	jCfg.Redis = "primary"
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": jCfg}

	builder := New(s)
	cmd, err := JobQueuesCommand("deploy:jobs-inspect", builder)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(cmd)
	if err != nil {
		t.Fatal(err)
	}

	// Usage errors return exit code 2 without touching Redis.
	for _, badArgs := range [][]string{
		{"deploy:jobs-inspect", "--unknown"},
		{"deploy:jobs-inspect", "--format", "yaml"},
		{"deploy:jobs-inspect", "--connection", "default"},
		{"deploy:jobs-inspect", "extra-positional"},
	} {
		if _, err := registry.Parse(badArgs, &bytes.Buffer{}); cli.Status(err) != cli.InvalidUsage {
			t.Fatalf("args %v: expected exit 2 (InvalidUsage), got %d (%v)", badArgs, cli.Status(err), err)
		}
	}

	// Unreachable Redis returns exit code 1 with redis_unavailable blocker and unknown/ineligible queue.
	inv, err := registry.Parse([]string{"deploy:jobs-inspect", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runErr := inv.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}})
	if cli.Status(runErr) != cli.Failure {
		t.Fatalf("expected exit 1 on unreachable Redis, got %d (%v)", cli.Status(runErr), runErr)
	}
	var report JobQueueReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Connections) != 1 || len(report.Connections[0].Queues) != 1 {
		t.Fatalf("unexpected connections on unreachable Redis: %+v", report.Connections)
	}
	if report.Connections[0].Queues[0].Layout != redis.JobQueueLayoutUnknown || report.Connections[0].Queues[0].Eligible {
		t.Fatalf("expected unknown/ineligible queue on unreachable Redis, got %+v", report.Connections[0].Queues[0])
	}
	expected := []JobQueueBlocker{
		{Code: JobQueueBlockerRedisUnavailable, Connection: value.Of(jobs.ConnectionName("default")), Queue: value.Null[jobs.Queue]()},
	}
	if !slices.Equal(report.Blockers, expected) {
		t.Fatalf("unexpected blockers on unreachable Redis: %+v", report.Blockers)
	}

	// Verify classifyQueueInspectionError maps non-transport script errors to layout_inspection_unsupported.
	if got := classifyQueueInspectionError(safeerror.NewFault(fault.Internal, "script unsupported")); got != JobQueueBlockerLayoutInspectionUnsupported {
		t.Fatalf("expected layout_inspection_unsupported, got %s", got)
	}
	if got := classifyQueueInspectionError(safeerror.NewFault(fault.Timeout, "timed out")); got != JobQueueBlockerRedisUnavailable {
		t.Fatalf("expected redis_unavailable, got %s", got)
	}
}
