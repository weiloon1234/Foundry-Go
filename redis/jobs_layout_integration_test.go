package redis

import (
	_ "embed"
	"encoding/json"
	"errors"
	"maps"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// Frozen v0.13.0 authority, byte-for-byte from 670db4ca:redis/jobs.lua. It proves
// the actual older writer rejects layout 3, without approximating its behavior.
//
//go:embed testdata/jobs_layout2.lua
var jobsLayout2Script string

func oldJobCommand(t *testing.T, backend *JobBackend, key jobs.Key, request jobRequest) int64 {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := evalScript(t.Context(), backend.client.raw, jobsLayout2Script, jobKeys(key)[:10], backend.identity, backend.limits, string(encoded), backend.legacy).Result()
	if err != nil {
		t.Fatal(err)
	}
	parts, ok := raw.([]any)
	if !ok || len(parts) == 0 {
		t.Fatal("invalid old script reply")
	}
	status, ok := parts[0].(int64)
	if !ok {
		t.Fatal("invalid old script status")
	}
	return status
}

func oldJobEnqueue(t *testing.T, f quarantineFixture, envelope jobs.Envelope) {
	t.Helper()
	data, err := envelope.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	request := jobRequest{Op: "enqueue", ID: envelope.ID().String(), Envelope: string(data), Name: envelope.Name(), Version: envelope.Version(), Maximum: envelope.Policy().Attempts, Bytes: int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))}
	if status := oldJobCommand(t, f.backend, f.key, request); status != 1 {
		t.Fatal("old enqueue", status)
	}
}

// R2 F4 and R3 mixed-version note: old operations cannot mutate quarantined state.
func TestRedisJobLayoutThreeCutoverFencesOldWriters(t *testing.T) {
	f := newQuarantineFixture(t)
	a := f.pending(t).Envelope()
	oldJobEnqueue(t, f, a)
	before := snapshotJobKeys(t, f.client, f.keys)
	if _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); !errors.Is(err, jobs.ErrLegacyLayout) {
		t.Fatal(err)
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("new client implicitly migrated queue")
	}
	metadata, err := f.client.raw.HGetAll(t.Context(), f.keys[4]).Result()
	if err != nil {
		t.Fatal(err)
	}
	// Migration must not parse this unreadable record or run maintenance.
	f.put(t, a.ID(), "corrupt old record")
	if ok, err := f.backend.JobMigrateLayout(t.Context(), f.key); err != nil || !ok {
		t.Fatal(ok, err)
	}
	metadata["layout"] = "3"
	after, err := f.client.raw.HGetAll(t.Context(), f.keys[4]).Result()
	if err != nil || !maps.Equal(metadata, after) {
		t.Fatal("migration changed accounting or time", err)
	}
	before = snapshotJobKeys(t, f.client, f.keys)
	if ok, err := f.backend.JobMigrateLayout(t.Context(), f.key); err != nil || ok {
		t.Fatal(ok, err)
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("repeat migration ran maintenance")
	}
	if got, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil || got.IsSet() {
		t.Fatal(err)
	}
	if len(f.quarantine(t).Records) != 1 {
		t.Fatal("old independent record not quarantined")
	}
	// Include the missing-envelope path that an unfenced old finish could mutate.
	if err := f.client.raw.HDel(t.Context(), f.keys[9], a.ID().String()).Err(); err != nil {
		t.Fatal(err)
	}
	before = snapshotJobKeys(t, f.client, f.keys)
	for _, op := range []string{"renew", "start", "finish", "forget", "enqueue", "list", "stats", "workflow_status", "cancel_workflow", "migrate_layout"} {
		status := oldJobCommand(t, f.backend, f.key, jobRequest{Op: op, ID: a.ID().String(), State: jobs.Succeeded, TTL: 60000, Limit: 100})
		if status != -2 {
			t.Fatal("old writer accepted layout 3", op, status)
		}
		if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
			t.Fatal("old writer changed layout 3", op)
		}
	}
}

func TestRedisJobLayoutThreeMigrationRejectsAmbiguousState(t *testing.T) {
	for _, damage := range []string{"policy", "unknown-layout", "counters", "key-type", "experimental-quarantine", "layout-one"} {
		t.Run(damage, func(t *testing.T) {
			f := newQuarantineFixture(t)
			a := f.pending(t).Envelope()
			oldJobEnqueue(t, f, a)
			switch damage {
			case "policy":
				if err := f.client.raw.HSet(t.Context(), f.keys[4], "config", "different").Err(); err != nil {
					t.Fatal(err)
				}
			case "unknown-layout":
				if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", "99").Err(); err != nil {
					t.Fatal(err)
				}
			case "counters":
				if err := f.client.raw.HSet(t.Context(), f.keys[4], "failed", 1).Err(); err != nil {
					t.Fatal(err)
				}
			case "key-type":
				if err := f.client.raw.Set(t.Context(), f.keys[10], "wrong type", 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "experimental-quarantine":
				if err := f.client.raw.ZAdd(t.Context(), f.keys[10], driver.Z{Score: 1, Member: a.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			case "layout-one":
				if err := f.client.raw.HDel(t.Context(), f.keys[4], "layout").Err(); err != nil {
					t.Fatal(err)
				}
				if err := f.client.raw.HSet(t.Context(), f.keys[4], "config", f.backend.legacy).Err(); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotJobKeys(t, f.client, f.keys)
			if ok, err := f.backend.JobMigrateLayout(t.Context(), f.key); err == nil || ok {
				t.Fatal("invalid migration accepted", ok, err)
			}
			if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
				t.Fatal("failed migration wrote state")
			}
		})
	}
}

func TestRedisJobLayoutCutoverNeedsNoMembershipBackfill(t *testing.T) {
	f := newQuarantineFixture(t)
	group := f.workflow(t, true, 1, true)
	independent := f.enqueue(t)
	// Layout 3 leaves record placement and the group schema identical to layout 2.
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", "2").Err(); err != nil {
		t.Fatal(err)
	}
	before := snapshotJobKeys(t, f.client, f.keys)
	if ok, err := f.backend.JobMigrateLayout(t.Context(), f.key); err != nil || !ok {
		t.Fatal(ok, err)
	}
	after := snapshotJobKeys(t, f.client, f.keys)
	delete(before, f.keys[4])
	delete(after, f.keys[4])
	if !maps.Equal(before, after) {
		t.Fatal("migration rewrote records or required a membership structure")
	}
	f.at(t, independent.ID(), 1)
	f.put(t, independent.ID(), "corrupt independent")
	if _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil {
		t.Fatal(err)
	}
	member := group.Envelope().Steps()[0]
	f.edit(t, member.ID(), map[string]any{"workflow": nil, "attempts": 0.5})
	f.unchanged(t, func() error {
		_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
		return err
	})
	if f.client.raw.ZCard(t.Context(), f.keys[10]).Val() != 1 {
		t.Fatal("old membership lost")
	}
}

func TestRedisJobLayoutCutoverCanResumeAcrossQueues(t *testing.T) {
	first, second := newQuarantineFixture(t), newQuarantineFixture(t)
	oldJobEnqueue(t, first, first.pending(t).Envelope())
	oldJobEnqueue(t, second, second.pending(t).Envelope())
	if _, err := first.backend.JobMigrateLayout(t.Context(), first.key); err != nil {
		t.Fatal(err)
	}
	if status := oldJobCommand(t, first.backend, first.key, jobRequest{Op: "stats"}); status != -2 {
		t.Fatal(status)
	}
	if _, err := second.backend.JobStats(t.Context(), second.key); !errors.Is(err, jobs.ErrLegacyLayout) {
		t.Fatal(err)
	}
	if _, err := second.backend.JobMigrateLayout(t.Context(), second.key); err != nil {
		t.Fatal(err)
	}
	if _, err := first.backend.JobStats(t.Context(), first.key); err != nil {
		t.Fatal(err)
	}
	if _, err := second.backend.JobStats(t.Context(), second.key); err != nil {
		t.Fatal(err)
	}
	// Empty queues initialize layout 3 and reject the old client as well.
	empty := newQuarantineFixture(t)
	if ok, err := empty.backend.JobMigrateLayout(t.Context(), empty.key); err != nil || ok {
		t.Fatal(ok, err)
	}
	if status := oldJobCommand(t, empty.backend, empty.key, jobRequest{Op: "stats"}); status != -2 {
		t.Fatal(status)
	}
	if _, err := empty.backend.JobCancel(t.Context(), empty.key, jobs.Target{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
