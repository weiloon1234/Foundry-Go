package redis

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
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
	request := oldJobEnvelopeRequest(t, envelope)
	request.Op = "enqueue"
	if status := oldJobCommand(t, f.backend, f.key, request); status != 1 {
		t.Fatal("old enqueue", status)
	}
}

func oldJobEnvelopeRequest(t *testing.T, envelope jobs.Envelope) jobRequest {
	t.Helper()
	data, err := envelope.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return jobRequest{ID: envelope.ID().String(), Envelope: string(data), Name: envelope.Name(), Version: envelope.Version(), Maximum: envelope.Policy().Attempts, Available: jobAvailableAt(envelope), Bytes: int64(len(data) + len(envelope.PayloadJSON()) + 8*len(envelope.Policy().Backoff))}
}

func oldJobBatch(t *testing.T, f quarantineFixture, count int) jobs.Workflow {
	t.Helper()
	steps := make([]jobs.Step, count)
	for i := range steps {
		steps[i] = f.pending(t).Step()
	}
	group, err := jobs.NewBatch(steps...)
	if err != nil {
		t.Fatal(err)
	}
	data, err := group.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	request := jobRequest{Op: "workflow", ID: group.ID().String(), WorkflowKind: group.Envelope().Kind(), Fingerprint: hex.EncodeToString(digest[:]), GroupBytes: int64(256 + count*32)}
	for _, member := range group.Envelope().Steps() {
		request.Steps = append(request.Steps, oldJobEnvelopeRequest(t, member))
	}
	if status := oldJobCommand(t, f.backend, f.key, request); status != 1 {
		t.Fatal("old workflow admission", status)
	}
	return group
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
	// Waiting members and the group membership schema are unchanged from layout 2.
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

func TestRedisJobLayoutThreeDetachesLegacyTerminalMembers(t *testing.T) {
	f := newQuarantineFixture(t)
	group := f.workflow(t, true, 2, false)
	member := group.Envelope().Steps()[0]
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", "2").Err(); err != nil {
		t.Fatal(err)
	}
	if status := oldJobCommand(t, f.backend, f.key, jobRequest{Op: "cancel", ID: member.ID().String(), Name: member.Name(), Version: member.Version()}); status != 1 {
		t.Fatal(status)
	}
	if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != 1 {
		t.Fatal("baseline did not create terminal placement")
	}
	before := snapshotJobKeys(t, f.client, f.keys)
	if ok, err := f.backend.JobMigrateLayout(t.Context(), f.key); err != nil || !ok {
		t.Fatal(ok, err)
	}
	after := snapshotJobKeys(t, f.client, f.keys)
	delete(before, f.keys[4])
	delete(after, f.keys[4])
	if !maps.Equal(before, after) {
		t.Fatal("migration rewrote legacy data")
	}
	charges := f.counters(t)
	f.advance(t, f.backend.config.Retention+time.Second)
	if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != 0 {
		t.Fatal("legacy terminal member not detached")
	}
	if !maps.Equal(charges, f.counters(t)) {
		t.Fatal("detachment changed accounting")
	}
	after = snapshotJobKeys(t, f.client, f.keys)
	delete(after, f.keys[4])
	delete(before, f.keys[3])
	delete(after, f.keys[3])
	if !maps.Equal(before, after) {
		t.Fatal("detachment changed records, envelopes or groups")
	}
	before = snapshotJobKeys(t, f.client, f.keys)
	if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("repeat detachment was not idempotent")
	}
}

func TestRedisJobLegacyDetachmentRollsBackBeforeReserveFailure(t *testing.T) {
	f := newQuarantineFixture(t)
	group := oldJobBatch(t, f, 2)
	done, ready := group.Envelope().Steps()[0], group.Envelope().Steps()[1]
	if status := oldJobCommand(t, f.backend, f.key, jobRequest{Op: "cancel", ID: done.ID().String(), Name: done.Name(), Version: done.Version()}); status != 1 {
		t.Fatal(status)
	}
	if _, err := f.backend.JobMigrateLayout(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	f.advance(t, f.backend.config.Retention+time.Second)
	f.edit(t, ready.ID(), map[string]any{"position": 0})
	f.unchanged(t, func() error { _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); return err })
	if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != 1 {
		t.Fatal("failed reserve committed legacy detachment")
	}
	f.edit(t, ready.ID(), map[string]any{"position": 1})
	before := f.counters(t)
	if got := f.reserve(t); got.Envelope.ID() != ready.ID() {
		t.Fatal("restored candidate not reserved")
	}
	if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != 0 || !maps.Equal(before, f.counters(t)) {
		t.Fatal("detachment did not commit without accounting changes")
	}
}

func TestRedisJobBaselineFullTerminalWindowsDetachWithProgress(t *testing.T) {
	for _, count := range []int{128, 512} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := newQuarantineFixture(t)
			if count == 512 {
				config := f.backend.config
				config.MaxRetained = 1
				var err error
				f.backend, err = NewJobBackend(f.client, config)
				if err != nil {
					t.Fatal(err)
				}
			}
			var members []jobs.Envelope
			for range count / 128 {
				group := oldJobBatch(t, f, 129)
				for _, member := range group.Envelope().Steps()[:128] {
					if status := oldJobCommand(t, f.backend, f.key, jobRequest{Op: "cancel", ID: member.ID().String(), Name: member.Name(), Version: member.Version()}); status != 1 {
						t.Fatal(status)
					}
					members = append(members, member)
				}
			}
			// Make the independent terminal entry strictly later than the full
			// member window, regardless of Redis clock resolution or UUID order.
			f.advance(t, time.Second)
			independent := f.pending(t).Envelope()
			oldJobEnqueue(t, f, independent)
			if status := oldJobCommand(t, f.backend, f.key, jobRequest{Op: "cancel", ID: independent.ID().String(), Name: independent.Name(), Version: independent.Version()}); status != 1 {
				t.Fatal(status)
			}
			if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != int64(count+1) {
				t.Fatal("baseline did not produce a full legacy window")
			}
			if ok, err := f.backend.JobMigrateLayout(t.Context(), f.key); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if count == 128 {
				f.advance(t, f.backend.config.Retention+time.Second)
			}
			before := f.counters(t)
			if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
				t.Fatal(err)
			}
			remaining, err := f.client.raw.ZRange(t.Context(), f.keys[3], 0, -1).Result()
			if err != nil || len(remaining) != 1 || remaining[0] != independent.ID().String() {
				t.Fatal("legacy window failed to advance", remaining, err)
			}
			if !maps.Equal(before, f.counters(t)) {
				t.Fatal("detachment refunded member accounting")
			}
			if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
				t.Fatal(err)
			}
			if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != 0 || f.client.raw.HExists(t.Context(), f.keys[0], independent.ID().String()).Val() {
				t.Fatal("later independent record was not retired")
			}
			if got := f.counters(t)["retained"]; got != strconv.Itoa(count) {
				t.Fatal("member charges changed", got)
			}
			for _, member := range members {
				if !f.client.raw.HExists(t.Context(), f.keys[0], member.ID().String()).Val() || !f.client.raw.HExists(t.Context(), f.keys[9], member.ID().String()).Val() {
					t.Fatal("detachment deleted a member")
				}
			}
		})
	}
}
