package redis

import (
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (f quarantineFixture) workflow(t *testing.T, batch bool, steps int, specials bool) jobs.Workflow {
	t.Helper()
	members := make([]jobs.Step, steps)
	for i := range members {
		members[i] = f.pending(t).Step()
	}
	var group jobs.Workflow
	var err error
	if batch {
		group, err = jobs.NewBatch(members...)
	} else {
		group, err = jobs.NewChain(members...)
	}
	if err != nil {
		t.Fatal(err)
	}
	if specials {
		group, err = group.WithCompletion(f.pending(t).Step())
		if err != nil {
			t.Fatal(err)
		}
		group, err = group.WithCatch(f.pending(t).Step())
		if err != nil {
			t.Fatal(err)
		}
		group, err = group.WithFinally(f.pending(t).Step())
		if err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	return group
}

// R1 F3: recoverable healthy leases are not frozen because a sibling is corrupt.
func TestRedisQuarantineDoesNotFreezeWorkflowLeaseWindow(t *testing.T) {
	f := newQuarantineFixture(t)
	group := f.workflow(t, true, 128, true)
	leases := make([]jobs.Reservation, 128)
	for i := range leases {
		leases[i] = f.reserve(t)
	}
	unrelated := f.enqueue(t)
	other := f.reserve(t)
	if other.Envelope.ID() != unrelated.ID() {
		t.Fatal("unexpected unrelated lease")
	}
	completion, _ := group.Envelope().Completion().Get()
	f.put(t, completion.ID(), "corrupt")
	for _, r := range leases {
		f.edit(t, r.Envelope.ID(), map[string]any{"expiry": 1})
		if err := f.client.raw.ZAdd(t.Context(), f.keys[2], driver.Z{Score: 1, Member: r.Envelope.ID().String()}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	f.edit(t, unrelated.ID(), map[string]any{"expiry": 2})
	if err := f.client.raw.ZAdd(t.Context(), f.keys[2], driver.Z{Score: 2, Member: unrelated.ID().String()}).Err(); err != nil {
		t.Fatal(err)
	}
	f.reserve(t)
	f.reserve(t)
	var row struct {
		State  jobs.State `json:"state"`
		Expiry int64      `json:"expiry"`
	}
	if err := json.Unmarshal([]byte(f.stored(t, 0, unrelated.ID())), &row); err != nil {
		t.Fatal(err)
	}
	if row.State != jobs.Waiting && !(row.State == jobs.Reserved && row.Expiry > 2) {
		t.Fatal("unrelated lease recovery starved", row)
	}
	if len(f.quarantine(t).Records) != 0 {
		t.Fatal("workflow member quarantined")
	}
}

// R1 F4 and R2 F1: a completed predecessor cannot become a refundable quarantine.
func TestRedisQuarantineCompletedWorkflowMemberRemainsStrict(t *testing.T) {
	f := newQuarantineFixture(t)
	group := f.workflow(t, false, 3, false)
	a := f.reserve(t)
	f.finish(t, a, jobs.Succeeded)
	b := f.reserve(t)
	if _, err := f.backend.JobStart(t.Context(), f.key, b.Ownership); err != nil {
		t.Fatal(err)
	}
	f.put(t, a.Envelope.ID(), "corrupt predecessor")
	before := f.counters(t)
	for range 2 {
		f.unchanged(t, func() error {
			_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
			return err
		})
		f.unchanged(t, func() error { _, err := f.backend.JobForget(t.Context(), f.key, a.Envelope.Target()); return err })
		f.unchanged(t, func() error { _, err := f.backend.JobWorkflowStatus(t.Context(), f.key, group.ID()); return err })
	}
	if !maps.Equal(before, f.counters(t)) || len(f.quarantine(t).Records) != 0 {
		t.Fatal("workflow corruption refunded accounting")
	}
	f.finish(t, b, jobs.Succeeded)
	if c := f.reserve(t); c.Envelope.ID() != group.Envelope().Steps()[2].ID() {
		t.Fatal("existing healthy advancement changed")
	}
}

// R3 F1: required corrupt dependants still fail closed, including maintenance.
func TestRedisQuarantineWorkflowExpiryAdvancementFailsClosed(t *testing.T) {
	f := newQuarantineFixture(t)
	group := f.workflow(t, false, 2, false)
	a := f.reserve(t)
	if _, err := f.backend.JobStart(t.Context(), f.key, a.Ownership); err != nil {
		t.Fatal(err)
	}
	b := group.Envelope().Steps()[1]
	original := f.stored(t, 0, b.ID())
	f.put(t, b.ID(), "corrupt blocked dependent")
	f.edit(t, a.Envelope.ID(), map[string]any{"attempts": a.Envelope.Policy().Attempts})
	f.advance(t, time.Minute+time.Second)
	for range 2 {
		f.unchanged(t, func() error { _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); return err })
		f.unchanged(t, func() error {
			_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
			return err
		})
	}
	if len(f.quarantine(t).Records) != 0 {
		t.Fatal("workflow dependent quarantined")
	}
	f.put(t, b.ID(), original)
	if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
		t.Fatal("restored group did not resume", err)
	}
	status, err := f.backend.JobWorkflowStatus(t.Context(), f.key, group.ID())
	value, ok := status.Get()
	if err != nil || !ok || !value.Finished() {
		t.Fatal(value, err)
	}
}

// R2 F3: missing records never lose the group that still owns their charges.
func TestRedisQuarantineGroupCleanupKeepsMissingMemberOwner(t *testing.T) {
	for _, missing := range []string{"record-only", "all-artifacts", "corrupt", "forced"} {
		t.Run(missing, func(t *testing.T) {
			f := newQuarantineFixture(t)
			if missing == "forced" {
				config := f.backend.config
				config.MaxRetained = 1
				var err error
				f.backend, err = NewJobBackend(f.client, config)
				if err != nil {
					t.Fatal(err)
				}
			}
			group := f.workflow(t, false, 1, false)
			member := group.Envelope().Steps()[0]
			if _, err := f.backend.JobCancelWorkflow(t.Context(), f.key, group.ID()); err != nil {
				t.Fatal(err)
			}
			if missing == "corrupt" {
				f.put(t, member.ID(), "corrupt")
			} else {
				if err := f.client.raw.HDel(t.Context(), f.keys[0], member.ID().String()).Err(); err != nil {
					t.Fatal(err)
				}
				if missing == "all-artifacts" {
					if err := f.client.raw.HDel(t.Context(), f.keys[9], member.ID().String()).Err(); err != nil {
						t.Fatal(err)
					}
					for _, i := range []int{1, 2, 3, 8} {
						if err := f.client.raw.ZRem(t.Context(), f.keys[i], member.ID().String()).Err(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if missing == "forced" {
				// Bypass the independent terminal scan to reach forced group cleanup.
				if err := f.client.raw.ZRem(t.Context(), f.keys[3], member.ID().String()).Err(); err != nil {
					t.Fatal(err)
				}
				// A healthy group's cancellation requires eviction of the older broken group.
				next := f.workflow(t, false, 1, false)
				f.unchanged(t, func() error { _, err := f.backend.JobCancelWorkflow(t.Context(), f.key, next.ID()); return err })
			} else {
				f.advance(t, f.backend.config.Retention+time.Second)
				f.unchanged(t, func() error { _, err := f.backend.JobStats(t.Context(), f.key); return err })
			}
			if !f.client.raw.HExists(t.Context(), f.keys[6], group.ID().String()).Val() || len(f.quarantine(t).Records) != 0 {
				t.Fatal("cleanup lost owner or quarantined member")
			}
		})
	}
}

// R3 F2: the 129th member remains physically owned until coherent group cleanup.
func TestRedisQuarantineExpiredWorkflowDeletionBelongsToGroup(t *testing.T) {
	for _, read := range []string{"inspect", "list", "status", "enqueue"} {
		t.Run(read, func(t *testing.T) {
			f := newQuarantineFixture(t)
			for range 129 {
				g := f.workflow(t, false, 1, false)
				if _, err := f.backend.JobCancelWorkflow(t.Context(), f.key, g.ID()); err != nil {
					t.Fatal(err)
				}
			}
			last, err := f.client.raw.ZRange(t.Context(), f.keys[7], 128, 128).Result()
			if err != nil || len(last) != 1 {
				t.Fatal(last, err)
			}
			text, err := f.client.raw.HGet(t.Context(), f.keys[6], last[0]).Result()
			if err != nil {
				t.Fatal(err)
			}
			var group struct {
				Members []jobs.ExecutionID `json:"members"`
			}
			if err := json.Unmarshal([]byte(text), &group); err != nil {
				t.Fatal(err)
			}
			encodedEnvelope, err := f.client.raw.HGet(t.Context(), f.keys[9], group.Members[0].String()).Result()
			if err != nil {
				t.Fatal(err)
			}
			member, err := jobs.DecodeEnvelope([]byte(encodedEnvelope))
			if err != nil {
				t.Fatal(err)
			}
			f.advance(t, f.backend.config.Retention+time.Second)
			switch read {
			case "enqueue":
				if inserted, err := f.backend.JobEnqueue(t.Context(), f.key, member); err != nil || inserted {
					t.Fatal("expired group member identity was reused", inserted, err)
				}

			case "inspect":
				found, err := f.backend.JobInspect(t.Context(), f.key, group.Members[0])
				if err != nil || found.IsSet() {
					t.Fatal(err)
				}
			case "list":
				if page := f.list(t); len(page.Records) != 0 {
					t.Fatal(page)
				}
			case "status":
				id, err := model.ParseID[jobs.WorkflowExecution](last[0])
				if err != nil {
					t.Fatal(err)
				}
				found, err := f.backend.JobWorkflowStatus(t.Context(), f.key, id)
				if err != nil || found.IsSet() {
					t.Fatal(err)
				}
			}
			if read != "status" && (!f.client.raw.HExists(t.Context(), f.keys[0], group.Members[0].String()).Val() || !f.client.raw.HExists(t.Context(), f.keys[6], last[0]).Val()) {
				t.Fatal("individual expiry deleted member")
			}
			stats, err := f.backend.JobStats(t.Context(), f.key)
			if err != nil || stats.Retained != 0 || stats.Quarantined != 0 || f.counters(t)["retained_bytes"] != "0" || f.client.raw.HLen(t.Context(), f.keys[6]).Val() != 0 {
				t.Fatal("group charges leaked", stats, err)
			}
		})
	}
}

// R3 F3: completed completion/callback facts are never replaced by quarantine.
func TestRedisQuarantineCompletedSpecialMembersFailClosed(t *testing.T) {
	for _, role := range []string{"completion", "catch", "finally", "missing-completion"} {
		t.Run(role, func(t *testing.T) {
			f := newQuarantineFixture(t)
			group := f.workflow(t, true, 1, true)
			completion, _ := group.Envelope().Completion().Get()
			catch, _ := group.Envelope().Catch().Get()
			finally, _ := group.Envelope().Finally().Get()
			var done jobs.Envelope
			var last jobs.Reservation
			if role == "completion" || role == "missing-completion" {
				f.finish(t, f.reserve(t), jobs.Succeeded)
				f.finish(t, f.reserve(t), jobs.Succeeded)
				done = completion
				last = f.reserve(t)
			} else {
				f.finish(t, f.reserve(t), jobs.Failed)
				first, second := catch, finally
				if role == "finally" {
					first, second = finally, catch
				}
				f.at(t, first.ID(), 1)
				f.at(t, second.ID(), 2)
				f.finish(t, f.reserve(t), jobs.Succeeded)
				done = first
				last = f.reserve(t)
			}
			if _, err := f.backend.JobStart(t.Context(), f.key, last.Ownership); err != nil {
				t.Fatal(err)
			}
			original := f.stored(t, 0, done.ID())
			f.put(t, done.ID(), "corrupt completed special")
			if role == "missing-completion" {
				for _, index := range []int{0, 9} {
					if err := f.client.raw.HDel(t.Context(), f.keys[index], done.ID().String()).Err(); err != nil {
						t.Fatal(err)
					}
				}
				for _, index := range []int{1, 2, 3, 8} {
					if err := f.client.raw.ZRem(t.Context(), f.keys[index], done.ID().String()).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if role != "missing-completion" {
				f.unchanged(t, func() error {
					_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
					return err
				})
			}
			f.unchanged(t, func() error {
				_, err := f.backend.JobFinish(t.Context(), f.key, last.Ownership, jobs.Result{State: jobs.Succeeded})
				return err
			})
			if len(f.quarantine(t).Records) != 0 {
				t.Fatal("special member quarantined")
			}
			f.put(t, done.ID(), original)
			if role == "missing-completion" {
				encoded, err := done.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				if err := f.client.raw.HSet(t.Context(), f.keys[9], done.ID().String(), encoded).Err(); err != nil {
					t.Fatal(err)
				}
			}
			f.finish(t, last, jobs.Succeeded)
			status, err := f.backend.JobWorkflowStatus(t.Context(), f.key, group.ID())
			v, ok := status.Get()
			if err != nil || !ok || !v.Finished() {
				t.Fatal(v, err)
			}
		})
	}
}

func TestRedisQuarantineProofCoversEveryWorkflowRole(t *testing.T) {
	for _, role := range []int{0, 1, 2, 3} {
		for _, damage := range []string{"json", "missing", "envelope", "erased-workflow", "oversized", "history"} {
			t.Run(strconv.Itoa(role)+"/"+damage, func(t *testing.T) {
				f := newQuarantineFixture(t)
				g := f.workflow(t, true, 1, true)
				member := g.Envelope().Members()[role]
				switch damage {
				case "json":
					f.put(t, member.ID(), "unreadable")
				case "missing":
					if err := f.client.raw.HDel(t.Context(), f.keys[0], member.ID().String()).Err(); err != nil {
						t.Fatal(err)
					}
				case "envelope":
					if err := f.client.raw.HDel(t.Context(), f.keys[9], member.ID().String()).Err(); err != nil {
						t.Fatal(err)
					}
				case "erased-workflow":
					f.edit(t, member.ID(), map[string]any{"workflow": nil, "attempts": 0.5})
				case "oversized":
					f.put(t, member.ID(), strings.Repeat("x", 3<<20))
				case "history":
					f.edit(t, member.ID(), map[string]any{"history": []int{42}})
				}
				f.unchanged(t, func() error {
					_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
					return err
				})
				if len(f.quarantine(t).Records) != 0 {
					t.Fatal("workflow role misclassified")
				}
			})
		}
	}
}

func TestRedisQuarantineProofBudgetsAndCatalogIntegrity(t *testing.T) {
	for _, count := range []int{0, 128, 129} {
		t.Run("groups-"+strconv.Itoa(count), func(t *testing.T) {
			f := newQuarantineFixture(t)
			for range count {
				f.workflow(t, false, 1, false)
			}
			bad := f.enqueue(t)
			f.at(t, bad.ID(), 1)
			f.put(t, bad.ID(), "corrupt")
			if count > 128 {
				f.unchanged(t, func() error { _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); return err })
			} else {
				if _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil {
					t.Fatal(err)
				}
				if f.client.raw.ZCard(t.Context(), f.keys[10]).Val() != 1 {
					t.Fatal("complete proof rejected")
				}
			}
		})
	}
	for _, budget := range []int{(1 << 20) - 1, 1 << 20, (1 << 20) + 1} {
		t.Run("bytes-"+strconv.Itoa(budget), func(t *testing.T) {
			f := newQuarantineFixture(t)
			groups := make([]jobs.Workflow, 17)
			for i := range groups {
				groups[i] = f.workflow(t, false, 1, false)
			}
			remaining := budget
			for i, g := range groups {
				raw, err := f.client.raw.HGet(t.Context(), f.keys[6], g.ID().String()).Result()
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal([]byte(raw), &fields); err != nil {
					t.Fatal(err)
				}
				fields["padding"] = ""
				encoded, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				size := remaining / (len(groups) - i)
				fields["padding"] = strings.Repeat("x", size-len(encoded))
				encoded, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				remaining -= len(encoded)
				if err := f.client.raw.HSet(t.Context(), f.keys[6], g.ID().String(), encoded).Err(); err != nil {
					t.Fatal(err)
				}
			}
			bad := f.enqueue(t)
			f.at(t, bad.ID(), 1)
			f.put(t, bad.ID(), "corrupt")
			_, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute)
			if budget > 1<<20 {
				if !errors.Is(err, fault.Invalid) || f.client.raw.ZCard(t.Context(), f.keys[10]).Val() != 0 {
					t.Fatal("incomplete proof accepted", err)
				}
			} else if err != nil || f.client.raw.ZCard(t.Context(), f.keys[10]).Val() != 1 {
				t.Fatal("bounded complete proof rejected", err)
			}
		})
	}
	for _, damage := range []string{"json", "oversized", "members-object", "duplicate", "missing-callbacks"} {
		t.Run(damage, func(t *testing.T) {
			f := newQuarantineFixture(t)
			g := f.workflow(t, false, 1, false)
			raw, err := f.client.raw.HGet(t.Context(), f.keys[6], g.ID().String()).Result()
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal([]byte(raw), &fields); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "json":
				raw = "invalid"
			case "oversized":
				raw = strings.Repeat("x", 65537)
			case "members-object":
				fields["members"] = map[string]string{"1": g.Envelope().Steps()[0].ID().String()}
			case "duplicate":
				fields["completion"] = g.Envelope().Steps()[0].ID().String()
			case "missing-callbacks":
				delete(fields, "catch")
				delete(fields, "finally")
			}
			if damage != "json" && damage != "oversized" {
				encoded, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				raw = string(encoded)
			}
			if err := f.client.raw.HSet(t.Context(), f.keys[6], g.ID().String(), raw).Err(); err != nil {
				t.Fatal(err)
			}
			bad := f.enqueue(t)
			f.at(t, bad.ID(), 1)
			f.put(t, bad.ID(), "corrupt")
			_, err = f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute)
			if damage == "missing-callbacks" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid catalog proved independence", err)
			}
		})
	}
}

func TestRedisQuarantineGroupIdentityReuseRemovesFinishedIndex(t *testing.T) {
	f := newQuarantineFixture(t)
	group := f.workflow(t, false, 1, false)
	if _, err := f.backend.JobCancelWorkflow(t.Context(), f.key, group.ID()); err != nil {
		t.Fatal(err)
	}
	f.advance(t, f.backend.config.Retention+time.Second)
	if ok, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := f.client.raw.ZScore(t.Context(), f.keys[7], group.ID().String()).Result(); !errors.Is(err, driver.Nil) {
		t.Fatal("reopened group kept finished index", err)
	}
	stats, err := f.backend.JobStats(t.Context(), f.key)
	if err != nil || stats.Waiting != 1 || stats.Retained != 0 || stats.Quarantined != 0 {
		t.Fatal(stats, err)
	}
	f.finish(t, f.reserve(t), jobs.Succeeded)
	f.advance(t, f.backend.config.Retention+time.Second)
	stats, err = f.backend.JobStats(t.Context(), f.key)
	if err != nil || stats.Retained != 0 || f.counters(t)["retained_bytes"] != "0" {
		t.Fatal("reused group leaked accounting", stats, err)
	}
}

func TestRedisQuarantineProofIncludesStagedGroupAndRollsBack(t *testing.T) {
	for _, count := range []int{127, 128} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := newQuarantineFixture(t)
			config := f.backend.config
			config.MaxRetained = 1
			var err error
			f.backend, err = NewJobBackend(f.client, config)
			if err != nil {
				t.Fatal(err)
			}
			for range count {
				f.workflow(t, false, 1, false)
			}
			a := f.enqueue(t)
			f.at(t, a.ID(), 1)
			f.finish(t, f.reserve(t), jobs.Succeeded)
			f.put(t, a.ID(), "corrupt A")
			b := f.enqueue(t)
			f.at(t, b.ID(), 1)
			f.finish(t, f.reserve(t), jobs.Succeeded)
			// Eviction quarantined A while B was not yet in the persisted finished index.
			if f.client.raw.ZCard(t.Context(), f.keys[10]).Val() != 1 {
				t.Fatal("A was not quarantined")
			}
			f.put(t, b.ID(), "corrupt B")
			group, err := jobs.NewChain(f.pending(t).Step())
			if err != nil {
				t.Fatal(err)
			}
			if count == 128 {
				f.unchanged(t, func() error { _, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); return err })
			} else {
				if ok, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); err != nil || !ok {
					t.Fatal(ok, err)
				}
				if f.client.raw.ZCard(t.Context(), f.keys[10]).Val() != 2 {
					t.Fatal("complete staged proof was rejected")
				}
			}
		})
	}
}

func TestRedisQuarantineMarkerPreventsWorkflowAdmission(t *testing.T) {
	f := newQuarantineFixture(t)
	pending := f.pending(t)
	if _, err := f.backend.JobEnqueue(t.Context(), f.key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	if err := f.client.raw.HDel(t.Context(), f.keys[0], pending.Envelope().ID().String()).Err(); err != nil {
		t.Fatal(err)
	}
	f.list(t)
	group, err := jobs.NewChain(pending.Step())
	if err != nil {
		t.Fatal(err)
	}
	f.unchanged(t, func() error { _, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); return err })
	if f.client.raw.HLen(t.Context(), f.keys[6]).Val() != 0 {
		t.Fatal("quarantined identity admitted into group")
	}
}
