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

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// Frozen v0.13.0 authority, byte-for-byte from 670db4ca:redis/jobs.lua. It seeds
// terminal workflow members with the original index placement for lazy-reader tests.
//
//go:embed testdata/jobs_layout2.lua
var jobsLayout2Script string

func TestRedisJobUnsupportedLayoutsRejectWithoutWrites(t *testing.T) {
	for _, layout := range []string{"", "1", "2", "99", "foreign"} {
		t.Run(layout, func(t *testing.T) {
			f := newQuarantineFixture(t)
			f.enqueue(t)
			if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", layout).Err(); err != nil {
				t.Fatal(err)
			}
			before := snapshotJobKeys(t, f.client, f.keys)
			inspection, err := f.backend.InspectLayout(t.Context(), f.key)
			blocker := JobLayoutBlockerUnsupportedLayout
			if layout == "" {
				blocker = JobLayoutBlockerInvalidState
			}
			if err != nil || inspection.Layout != JobQueueLayoutUnknown || inspection.Eligible || inspection.Blocker != blocker {
				t.Fatal("unsupported layout inspection", inspection, err)
			}
			if _, err := f.backend.JobStats(t.Context(), f.key); !errors.Is(err, fault.Invalid) {
				t.Fatal("unsupported layout accepted by stats", err)
			}
			if _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); !errors.Is(err, fault.Invalid) {
				t.Fatal("unsupported layout accepted by reserve", err)
			}
			if _, err := f.backend.JobEnqueue(t.Context(), f.key, f.pending(t).Envelope()); !errors.Is(err, fault.Invalid) {
				t.Fatal("unsupported layout accepted by enqueue", err)
			}
			if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
				t.Fatal("unsupported layout changed queue state")
			}
		})
	}
}

func oldJobCommand(t *testing.T, backend *JobBackend, key jobs.Key, request jobRequest) int64 {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := evalScript(t.Context(), backend.client.raw, jobsLayout2Script, jobKeys(key)[:10], backend.identity, backend.limits, string(encoded), "").Result()
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
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", "3").Err(); err != nil {
		t.Fatal(err)
	}
	after := snapshotJobKeys(t, f.client, f.keys)
	delete(before, f.keys[4])
	delete(after, f.keys[4])
	if !maps.Equal(before, after) {
		t.Fatal("fixture changed legacy data")
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
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", "3").Err(); err != nil {
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
			if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", "3").Err(); err != nil {
				t.Fatal(err)
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

func TestRedisJobInspectLayoutIsReadOnlyAndClassifiesBlockers(t *testing.T) {
	f := newQuarantineFixture(t)

	// 1. Absent queue: eligible, no keys created by inspection.
	beforeAbsent := snapshotJobKeys(t, f.client, f.keys)
	insp, err := f.backend.InspectLayout(t.Context(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Layout != JobQueueLayoutAbsent || !insp.Eligible || insp.Blocker != JobLayoutBlockerNone {
		t.Fatalf("unexpected absent inspection: %+v", insp)
	}
	if !maps.Equal(beforeAbsent, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("InspectLayout modified an absent queue")
	}

	// 2. Layout 3 queue: eligible, read-only.
	_ = f.enqueue(t)
	beforeThree := snapshotJobKeys(t, f.client, f.keys)
	insp, err = f.backend.InspectLayout(t.Context(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Layout != JobQueueLayout3 || !insp.Eligible || insp.Blocker != JobLayoutBlockerNone {
		t.Fatalf("unexpected layout 3 inspection: %+v", insp)
	}
	if !maps.Equal(beforeThree, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("InspectLayout modified a layout 3 queue")
	}

	// 5. Unsupported layout: unknown + unsupported_layout.
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "config", f.backend.identity, "layout", "99").Err(); err != nil {
		t.Fatal(err)
	}
	insp, err = f.backend.InspectLayout(t.Context(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Layout != JobQueueLayoutUnknown || insp.Eligible || insp.Blocker != JobLayoutBlockerUnsupportedLayout {
		t.Fatalf("unexpected unsupported layout inspection: %+v", insp)
	}

	// 6. Policy mismatch: unknown + queue_policy_mismatch.
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "config", `{"MaxEntries":1}`, "layout", "3").Err(); err != nil {
		t.Fatal(err)
	}
	insp, err = f.backend.InspectLayout(t.Context(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Layout != JobQueueLayoutUnknown || insp.Eligible || insp.Blocker != JobLayoutBlockerPolicyMismatch {
		t.Fatalf("unexpected policy mismatch inspection: %+v", insp)
	}

	// 7. Invalid queue state (corrupt counter): unknown + invalid_queue_state.
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "config", f.backend.identity, "layout", "3", "failed", 99).Err(); err != nil {
		t.Fatal(err)
	}
	insp, err = f.backend.InspectLayout(t.Context(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Layout != JobQueueLayoutUnknown || insp.Eligible || insp.Blocker != JobLayoutBlockerInvalidState {
		t.Fatalf("unexpected invalid state inspection: %+v", insp)
	}
}
