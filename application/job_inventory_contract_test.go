package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/inspection"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

// Assert raw, case-sensitive keys independently of the production Go DTOs.
func inventoryWireObject(t *testing.T, wire []byte, fields ...string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(wire, &object); err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(object))
	slices.Sort(fields)
	if !slices.Equal(got, fields) {
		t.Fatalf("wire fields = %v, want %v", got, fields)
	}
	return object
}

func TestJobQueuesSchemaOneRawJSON(t *testing.T) {
	report, err := New(jobInventorySettings()).JobQueues()
	if err != nil {
		t.Fatal(err)
	}
	report.Blockers = []JobQueueBlocker{{Code: JobQueueBlockerInvalidConfiguration}}
	var output bytes.Buffer
	if err := WriteJobQueues(&output, inspection.JSON, report); err != nil {
		t.Fatal(err)
	}
	root := inventoryWireObject(t, output.Bytes(), "schema", "supported_layout", "coverage", "connections", "blockers")
	if string(root["schema"]) != "1" || string(root["supported_layout"]) != "3" || string(root["coverage"]) != `"declared"` {
		t.Fatal("invalid schema envelope")
	}
	var connections, blockers []json.RawMessage
	if err := json.Unmarshal(root["connections"], &connections); err != nil {
		t.Fatal(err)
	}
	if len(connections) != 1 {
		t.Fatal("expected one connection")
	}
	conn := inventoryWireObject(t, connections[0], "connection", "redis_connection", "storage_id", "namespace", "prefix", "policy_id", "queues")
	ns := inventoryWireObject(t, conn["namespace"], "application", "environment")
	if string(ns["application"]) != `"foundry-app.jobs.default"` || string(ns["environment"]) != `"production"` {
		t.Fatalf("namespace = %s", conn["namespace"])
	}
	var queues []json.RawMessage
	if err := json.Unmarshal(conn["queues"], &queues); err != nil {
		t.Fatal(err)
	}
	if len(queues) != 1 {
		t.Fatal("expected one queue")
	}
	queue := inventoryWireObject(t, queues[0], "queue", "base", "keys", "layout", "eligible")
	var keys []string
	if err := json.Unmarshal(queue["keys"], &keys); err != nil || len(keys) == 0 {
		t.Fatal("physical keys must be a nonempty string array", err)
	}
	if err := json.Unmarshal(root["blockers"], &blockers); err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 {
		t.Fatal("expected one blocker")
	}
	blocker := inventoryWireObject(t, blockers[0], "code", "connection", "queue")
	if string(blocker["connection"]) != "null" || string(blocker["queue"]) != "null" {
		t.Fatal("global blocker must use null selectors")
	}
}

func TestJobQueuesListenerDestinations(t *testing.T) {
	for _, tc := range []struct {
		name           string
		connection     jobs.ConnectionName
		queue          jobs.Queue
		wantConnection jobs.ConnectionName
		wantQueues     []jobs.Queue
		invalid        bool
	}{
		{name: "blank", wantConnection: "primary", wantQueues: []jobs.Queue{"default", "jobs"}},
		{name: "default_connection", queue: "events", wantConnection: "primary", wantQueues: []jobs.Queue{"events", "jobs"}},
		{name: "explicit_default", connection: "primary", queue: "default", wantConnection: "primary", wantQueues: []jobs.Queue{"default", "jobs"}},
		{name: "named_connection", connection: "secondary", queue: "events", wantConnection: "secondary", wantQueues: []jobs.Queue{"events", "jobs"}},
		{name: "invalid_connection", connection: "missing", queue: "events", invalid: true},
		{name: "invalid_queue", queue: "bad queue", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := jobInventorySettings()
			conn := s.Services.Jobs.Connections["default"]
			conn.DefaultQueue = "jobs"
			s.Services.Jobs = infrastructure.JobsSettings{Default: "primary", Connections: infrastructure.JobConnections{"primary": conn, "secondary": conn}}
			s.Features.Events.Enabled, s.Features.Events.QueuedListeners = true, true
			s.Features.Events.ListenerConnection, s.Features.Events.ListenerQueue = tc.connection, tc.queue
			report, err := New(s).JobQueues()
			if tc.invalid {
				if err == nil || len(report.Blockers) != 1 || report.Blockers[0].Code != JobQueueBlockerInvalidConfiguration {
					t.Fatalf("invalid listener was not blocked: %+v, %v", report, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range report.Connections {
				want := []jobs.Queue{"jobs"}
				if c.Connection == tc.wantConnection {
					want = tc.wantQueues
				}
				var got []jobs.Queue
				for _, q := range c.Queues {
					got = append(got, q.Queue)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("%s queues = %v, want %v", c.Connection, got, want)
				}
			}
		})
	}
}

func TestJobQueuesFrameworkDestinations(t *testing.T) {
	s := jobInventorySettings()
	primary := s.Services.Jobs.Connections["default"]
	primary.DefaultQueue = "jobs"
	s.Services.Jobs.Connections["default"] = primary
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
	s.Features.Events.Enabled, s.Features.Events.QueuedListeners = true, true
	s.Features.Events.ListenerQueue = "listeners"
	s.Features.Outbox.Enabled, s.Features.Extensions.Enabled = true, true
	s.Features.ExtensionCleanup.Jobs = "default"
	s.Features.Outbox.Jobs = map[jobs.ConnectionName]outbox.Destination{"default": "cleanup"}
	s.Features.Outbox.Events = map[outbox.Destination]jobs.ConnectionName{"publications": "events", "also-publications": "events"}
	conn := s.Services.Jobs.Connections["default"]
	conn.DefaultQueue = "publications"
	s.Services.Jobs.Connections["events"] = conn
	report, err := New(s).Events(
		EventOutbox(foundation.NewKey[*events.Outbox]("inventory.publications"), "publications"),
		EventOutbox(foundation.NewKey[*events.Outbox]("inventory.also-publications"), "also-publications"),
	).JobQueues()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Connections) != 2 {
		t.Fatalf("connections = %+v", report.Connections)
	}
	for i, want := range [][]jobs.Queue{{"default", "jobs", "listeners"}, {"publications"}} {
		var got []jobs.Queue
		for _, q := range report.Connections[i].Queues {
			got = append(got, q.Queue)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("framework queues = %v, want %v", got, want)
		}
	}
}

type inventoryFailWriter struct{}

func (inventoryFailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestJobQueuesOutputFailuresAndByteBoundary(t *testing.T) {
	report := emptyJobQueueReport()
	if err := WriteJobQueues(inventoryFailWriter{}, inspection.JSON, report); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	report.Connections = []JobConnectionInventory{{Prefix: "", Queues: []JobQueueInventory{}}}
	initial, _, err := encodeJobQueueReport(report, inspection.JSON)
	if err != nil {
		t.Fatal(err)
	}
	report.Connections[0].Prefix = strings.Repeat("x", MaxJobQueueInventoryBytes-len(initial))
	atLimit, exceeded, err := encodeJobQueueReport(report, inspection.JSON)
	if err != nil || exceeded || len(atLimit) != MaxJobQueueInventoryBytes {
		t.Fatal("exact byte limit rejected", len(atLimit), exceeded, err)
	}
	report.Connections[0].Prefix += "x"
	var output bytes.Buffer
	if err := WriteJobQueues(&output, inspection.JSON, report); err == nil {
		t.Fatal("oversize output succeeded")
	}
	var limited JobQueueReport
	if err := json.Unmarshal(output.Bytes(), &limited); err != nil {
		t.Fatal(err)
	}
	if len(limited.Connections) != 0 || len(limited.Blockers) != 1 || limited.Blockers[0].Code != JobQueueBlockerInventoryLimit {
		t.Fatalf("oversize output did not fail closed: %+v", limited)
	}
}
