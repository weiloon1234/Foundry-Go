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
)

// Release F1: Lua must reject unknown keys before Go's case-insensitive decode.
func TestRedisQuarantineRejectsFieldAliases(t *testing.T) {
	for _, field := range []string{"At", "AT", "State", "Attempt", "Retry", "Reason", "unknown"} {
		for _, path := range []string{"list", "reserve"} {
			t.Run("history/"+field+"/"+path, func(t *testing.T) {
				f := newQuarantineFixture(t)
				bad := f.enqueue(t)
				f.at(t, bad.ID(), 1)
				good := f.enqueue(t)
				entry := map[string]any{"state": "waiting", "reason": "", "at": 1, "attempt": 0, field: "bad"}
				f.edit(t, bad.ID(), map[string]any{"history": []any{entry}})
				f.assertUnavailable(t, bad)
				if path == "list" {
					page := f.list(t)
					if len(page.Records) != 1 || page.Records[0].Envelope.ID() != good.ID() {
						t.Fatal("alias escaped listing")
					}
				} else if got := f.reserve(t); got.Envelope.ID() != good.ID() {
					t.Fatal("alias received a committed lease")
				}
				if page := f.quarantine(t); len(page.Records) != 1 || page.Records[0].ID != bad.ID() {
					t.Fatal("alias was not quarantined", page)
				}
			})
		}
	}
	for _, field := range []string{"ID", "History", "Available", "Expiry", "Created", "Finished", "Attempts", "Workflow", "Position", "unknown"} {
		for _, path := range []string{"list", "reserve"} {
			t.Run("record/"+field+"/"+path, func(t *testing.T) {
				f := newQuarantineFixture(t)
				e := f.enqueue(t)
				f.edit(t, e.ID(), map[string]any{field: "bad"})
				f.assertUnavailable(t, e)
				if path == "list" {
					if len(f.list(t).Records) != 0 {
						t.Fatal("alias listed")
					}
				} else if got, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil || got.IsSet() {
					t.Fatal("alias reserved", err)
				}
				if len(f.quarantine(t).Records) != 1 {
					t.Fatal("alias not quarantined")
				}
			})
		}
	}
}

// Release F2: removal of both ownership fields still leaves the group authoritative.
func TestRedisQuarantineIndividualDeletionRequiresProof(t *testing.T) {
	for _, damage := range []string{"workflow", "both"} {
		t.Run("forget/"+damage, func(t *testing.T) {
			f := newQuarantineFixture(t)
			group := f.workflow(t, false, 1, false)
			member := group.Envelope().Steps()[0]
			f.finish(t, f.reserve(t), jobs.Succeeded)
			fields := map[string]any{"workflow": nil}
			if damage == "both" {
				fields["position"] = nil
			}
			f.edit(t, member.ID(), fields)
			f.unchanged(t, func() error { _, err := f.backend.JobForget(t.Context(), f.key, member.Target()); return err })
			if len(f.quarantine(t).Records) != 0 {
				t.Fatal("member quarantined")
			}
		})
	}
	for _, path := range []string{"inspect", "list", "retention", "eviction"} {
		t.Run(path, func(t *testing.T) {
			f := newQuarantineFixture(t)
			if path == "eviction" {
				config := f.backend.config
				config.MaxRetained = 1
				var err error
				f.backend, err = NewJobBackend(f.client, config)
				if err != nil {
					t.Fatal(err)
				}
			}
			f.workflow(t, true, 2, false)
			r := f.reserve(t)
			f.finish(t, r, jobs.Succeeded)
			finished := int64(1)
			if path == "eviction" {
				independent := f.enqueue(t)
				f.at(t, independent.ID(), 1)
				f.finish(t, f.reserve(t), jobs.Succeeded)
				var row jobStoredRecord
				if err := json.Unmarshal([]byte(f.stored(t, 0, r.Envelope.ID())), &row); err != nil {
					t.Fatal(err)
				}
				finished = row.Finished - 1
			}
			f.edit(t, r.Envelope.ID(), map[string]any{"workflow": nil, "position": nil, "finished": finished})
			if path == "retention" || path == "eviction" {
				if err := f.client.raw.ZAdd(t.Context(), f.keys[3], driver.Z{Score: float64(finished), Member: r.Envelope.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			f.unchanged(t, func() error {
				if path == "inspect" {
					_, err := f.backend.JobInspect(t.Context(), f.key, r.Envelope.ID())
					return err
				}
				if path == "list" {
					_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
					return err
				}
				_, err := f.backend.JobStats(t.Context(), f.key)
				return err
			})
		})
	}
	t.Run("unknown", func(t *testing.T) {
		f := newQuarantineFixture(t)
		for range 129 {
			f.workflow(t, false, 1, false)
		}
		e := f.enqueue(t)
		f.at(t, e.ID(), 1)
		f.finish(t, f.reserve(t), jobs.Succeeded)
		f.unchanged(t, func() error { _, err := f.backend.JobForget(t.Context(), f.key, e.Target()); return err })
	})
}

func TestRedisQuarantineWorkflowPositionMismatch(t *testing.T) {
	for _, damage := range []string{"position", "workflow", "orphan"} {
		t.Run(damage, func(t *testing.T) {
			f := newQuarantineFixture(t)
			group := f.workflow(t, false, 2, false)
			other := f.workflow(t, false, 1, false)
			member := group.Envelope().Steps()[0]
			fields := map[string]any{"position": 1}
			if damage == "workflow" {
				fields = map[string]any{"workflow": other.ID().String()}
			}
			if damage == "orphan" {
				fields = map[string]any{"workflow": nil}
			}
			f.edit(t, member.ID(), fields)
			f.unchanged(t, func() error { _, err := f.backend.JobCancel(t.Context(), f.key, member.Target()); return err })
			f.unchanged(t, func() error { _, err := f.backend.JobWorkflowStatus(t.Context(), f.key, group.ID()); return err })
			f.unchanged(t, func() error {
				_, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100})
				return err
			})
		})
	}
}

// Release F3: retired G and newly admitted G must have distinct staged lifetimes.
func TestRedisQuarantineRecreatedGroupUnderRetainedPressure(t *testing.T) {
	f := newQuarantineFixture(t)
	config := f.backend.config
	config.MaxRetained = 1
	var err error
	f.backend, err = NewJobBackend(f.client, config)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		e := f.enqueue(t)
		f.finish(t, f.reserve(t), jobs.Succeeded)
		f.put(t, e.ID(), "corrupt terminal")
		f.list(t)
	}
	group := f.workflow(t, false, 1, false)
	f.finish(t, f.reserve(t), jobs.Succeeded)
	f.advance(t, config.Retention+time.Second)
	if ok, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := f.client.raw.ZScore(t.Context(), f.keys[7], group.ID().String()).Result(); !errors.Is(err, driver.Nil) {
		t.Fatal("recreated group retained old finished placement", err)
	}
	if len(f.quarantine(t).Records) != 2 {
		t.Fatal("quarantine lost")
	}
	f.finish(t, f.reserve(t), jobs.Succeeded)
}

// Release F4: group-owned terminal members never occupy an independent scan window.
func TestRedisQuarantineTerminalMembersDoNotPinMaintenance(t *testing.T) {
	for _, path := range []string{"retention", "eviction"} {
		for _, legacy := range []bool{false, true} {
			t.Run(path+"/legacy-"+strconv.FormatBool(legacy), func(t *testing.T) {
				f := newQuarantineFixture(t)
				count, batches := 128, 1
				if path == "eviction" {
					count, batches = 256, 2
					config := f.backend.config
					config.MaxRetained = 1
					var err error
					f.backend, err = NewJobBackend(f.client, config)
					if err != nil {
						t.Fatal(err)
					}
				}
				var members []jobs.Envelope
				for range batches {
					group := f.workflow(t, true, count, true)
					completion, _ := group.Envelope().Completion().Get()
					f.edit(t, completion.ID(), map[string]any{"available": maxJobInteger})
					claims := make([]jobs.Reservation, count)
					for i := range claims {
						claims[i] = f.reserve(t)
					}
					for _, claim := range claims {
						f.finish(t, claim, jobs.Succeeded)
						members = append(members, claim.Envelope)
					}
				}
				if n := f.client.raw.ZCard(t.Context(), f.keys[3]).Val(); n != 0 {
					t.Fatal("workflow members in independent index", n)
				}
				independent := f.enqueue(t)
				f.finish(t, f.reserve(t), jobs.Succeeded)
				if legacy {
					// Simulate layout-2 placement after the metadata-only migration.
					for _, member := range members {
						var row jobStoredRecord
						if err := json.Unmarshal([]byte(f.stored(t, 0, member.ID())), &row); err != nil {
							t.Fatal(err)
						}
						if err := f.client.raw.ZAdd(t.Context(), f.keys[3], driver.Z{Score: float64(row.Finished), Member: member.ID().String()}).Err(); err != nil {
							t.Fatal(err)
						}
					}
				}
				if path == "retention" {
					f.advance(t, f.backend.config.Retention+time.Second)
				}
				// A legacy full window may need one detachment call before the
				// later independent entry fits. Every call makes bounded progress.
				for range 2 {
					if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
						t.Fatal(err)
					}
				}
				if f.client.raw.HExists(t.Context(), f.keys[0], independent.ID().String()).Val() {
					t.Fatal("independent retention pinned")
				}
				if f.client.raw.ZCard(t.Context(), f.keys[3]).Val() != 0 {
					t.Fatal("legacy members not detached")
				}
				for _, member := range members {
					if !f.client.raw.HExists(t.Context(), f.keys[0], member.ID().String()).Val() {
						t.Fatal("group member deleted")
					}
				}
				if got := f.counters(t)["retained"]; got != strconv.Itoa(count*batches) {
					t.Fatal("group member charges changed", got)
				}
			})
		}
	}
}

// Release F5: the greatest admitted timestamp must survive actual cjson and Go.
func TestRedisQuarantineMaximumIntegerRoundTrip(t *testing.T) {
	f := newQuarantineFixture(t)
	e := f.enqueue(t)
	f.edit(t, e.ID(), map[string]any{"created": maxJobInteger, "history": []any{map[string]any{"state": "waiting", "reason": "", "at": maxJobInteger, "attempt": 0}}})
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "time", maxJobInteger-60_000).Err(); err != nil {
		t.Fatal(err)
	}
	r := f.reserve(t)
	if r.ExpiresAt.UnixMilli() != maxJobInteger {
		t.Fatal("lease timestamp rounded", r.ExpiresAt)
	}
	page := f.list(t)
	if len(page.Records) != 1 || page.Records[0].CreatedAt.UnixMilli() != maxJobInteger || page.Records[0].History[0].At.UnixMilli() != maxJobInteger {
		t.Fatal("history timestamp rounded")
	}
	if _, err := f.backend.JobStart(t.Context(), f.key, r.Ownership); err != nil {
		t.Fatal(err)
	}
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "time", maxJobInteger).Err(); err != nil {
		t.Fatal(err)
	}
	// Expiry writes history at the boundary; cancellation then writes a terminal time.
	if _, err := f.backend.JobCancel(t.Context(), f.key, e.Target()); err != nil {
		t.Fatal(err)
	}
	found, err := f.backend.JobInspect(t.Context(), f.key, e.ID())
	record, ok := found.Get()
	if err != nil || !ok || record.FinishedAt.UnixMilli() != maxJobInteger {
		t.Fatal("terminal timestamp failed round trip", err)
	}
	bad := f.enqueue(t)
	f.put(t, bad.ID(), "corrupt")
	f.list(t)
	if page := f.quarantine(t); len(page.Records) != 1 || page.Records[0].QuarantinedAt.UnixMilli() != maxJobInteger {
		t.Fatal("quarantine timestamp rounded")
	}
	// A new lease would overflow the wire range; the staged write must abort.
	healthy := f.enqueue(t)
	f.unchanged(t, func() error { _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); return err })
	if got := f.stored(t, 0, healthy.ID()); !strings.Contains(got, `"state":"waiting"`) {
		t.Fatal("overflow committed a lease")
	}
}

func TestRedisQuarantineRejectsOutOfRangeNumbers(t *testing.T) {
	for _, field := range []string{"available", "created", "expiry", "finished", "history"} {
		t.Run(field, func(t *testing.T) {
			f := newQuarantineFixture(t)
			e := f.enqueue(t)
			var value any = maxJobInteger + 1
			if field == "history" {
				value = []any{map[string]any{"state": "waiting", "reason": "", "at": maxJobInteger + 1, "attempt": 0}}
			}
			f.edit(t, e.ID(), map[string]any{field: value})
			f.assertUnavailable(t, e)
			f.list(t)
			if len(f.quarantine(t).Records) != 1 {
				t.Fatal("out of range record not quarantined")
			}
		})
	}
	f := newQuarantineFixture(t)
	if _, err := f.backend.command(t.Context(), f.key, jobRequest{Op: "enqueue", Available: maxJobInteger + 1}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if (jobReply{Record: &jobStoredRecord{Created: maxJobInteger + 1}}).validNumbers() {
		t.Fatal("Go reply boundary admitted excessive integer")
	}
	if (jobReply{Stats: &jobs.QueueStats{Waiting: -1}}).validNumbers() {
		t.Fatal("Go reply boundary admitted negative integer")
	}
	if f.client.raw.Exists(t.Context(), f.keys...).Val() != 0 {
		t.Fatal("invalid request initialized queue")
	}
}

func TestRedisQuarantineRollsBackBeforeWorkflowAndResponseFailure(t *testing.T) {
	for _, failure := range []string{"workflow", "response"} {
		t.Run(failure, func(t *testing.T) {
			f := newQuarantineFixture(t)
			bad := f.enqueue(t)
			f.finish(t, f.reserve(t), jobs.Succeeded)
			group := f.workflow(t, false, 2, false)
			if failure == "workflow" {
				f.put(t, group.Envelope().Steps()[1].ID(), "corrupt successor")
			}
			f.put(t, bad.ID(), "corrupt independent")
			f.advance(t, f.backend.config.Retention+time.Second)
			if failure == "response" {
				// Maintenance stages quarantine; stats later validates this same
				// affected identity's corrupt ready score before encoding its reply.
				if err := f.client.raw.ZAdd(t.Context(), f.keys[1], driver.Z{Score: -1, Member: bad.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			f.unchanged(t, func() error {
				if failure == "workflow" {
					_, err := f.backend.JobCancelWorkflow(t.Context(), f.key, group.ID())
					return err
				}
				_, err := f.backend.JobStats(t.Context(), f.key)
				return err
			})
			if len(f.quarantine(t).Records) != 0 {
				t.Fatal("failure committed staged quarantine")
			}
		})
	}
}

func TestRedisQuarantineFullReadyWindowWithStagedEntries(t *testing.T) {
	for _, state := range []string{"dirty", "dirty-tie", "removed"} {
		t.Run(state, func(t *testing.T) {
			f := newQuarantineFixture(t)
			var entries []jobs.Envelope
			if state == "removed" {
				group := f.workflow(t, true, 128, false)
				entries = group.Envelope().Steps()
				for range 128 {
					f.finish(t, f.reserve(t), jobs.Succeeded)
				}
			} else {
				for range 128 {
					entries = append(entries, f.enqueue(t))
				}
				for range 128 {
					f.reserve(t)
				}
			}
			// Admission must precede expiry injection: it also runs maintenance.
			unseen := f.enqueue(t)
			if state != "removed" {
				for _, e := range entries {
					f.edit(t, e.ID(), map[string]any{"expiry": 1})
					if err := f.client.raw.ZAdd(t.Context(), f.keys[2], driver.Z{Score: 1, Member: e.ID().String()}).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.advance(t, f.backend.config.Retention+time.Second)
			at := int64(2)
			if state == "dirty-tie" {
				var err error
				at, err = f.client.raw.HGet(t.Context(), f.keys[4], "time").Int64()
				if err != nil {
					t.Fatal(err)
				}
			}
			f.at(t, unseen.ID(), at)
			for _, e := range entries {
				if err := f.client.raw.ZAdd(t.Context(), f.keys[1], driver.Z{Score: 1, Member: e.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotJobKeys(t, f.client, f.keys)
			if got, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil || got.IsSet() {
				t.Fatal("full staged window reserved unseen work", err)
			}
			if maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
				t.Fatal("empty reserve failed to commit progress")
			}
			want := unseen.ID()
			if state == "dirty-tie" {
				for _, e := range entries {
					if e.ID().String() < want.String() {
						want = e.ID()
					}
				}
			}
			if got := f.reserve(t); got.Envelope.ID() != want {
				t.Fatal("staged ordering lost", got.Envelope.ID(), want)
			}
		})
	}
}

func TestRedisQuarantineCompletedProofExtendsAcrossByteBudget(t *testing.T) {
	for _, remaining := range []int{2000, 1} {
		t.Run(strconv.Itoa(remaining), func(t *testing.T) {
			f := newQuarantineFixture(t)
			config := f.backend.config
			config.MaxRetained = 1
			var err error
			f.backend, err = NewJobBackend(f.client, config)
			if err != nil {
				t.Fatal(err)
			}
			groups := make([]jobs.Workflow, 17)
			for i := range groups {
				groups[i] = f.workflow(t, false, 1, false)
			}
			a := f.enqueue(t)
			f.at(t, a.ID(), 1)
			f.finish(t, f.reserve(t), jobs.Succeeded)
			// Temporarily withhold this owned test entry from eviction, then
			// restore it before the invocation under test.
			if err := f.client.raw.ZRem(t.Context(), f.keys[3], a.ID().String()).Err(); err != nil {
				t.Fatal(err)
			}
			b := f.enqueue(t)
			f.at(t, b.ID(), 1)
			f.finish(t, f.reserve(t), jobs.Succeeded)
			if err := f.client.raw.ZAdd(t.Context(), f.keys[3], driver.Z{Score: 1, Member: a.ID().String()}).Err(); err != nil {
				t.Fatal(err)
			}
			f.put(t, a.ID(), "corrupt due record")
			f.put(t, b.ID(), "corrupt eviction record")
			padQuarantineCatalog(t, f, groups, (1<<20)-remaining)
			group, err := jobs.NewChain(f.pending(t).Step())
			if err != nil {
				t.Fatal(err)
			}
			if remaining == 1 {
				f.unchanged(t, func() error { _, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); return err })
				if len(f.quarantine(t).Records) != 0 {
					t.Fatal("invalidated proof committed quarantine")
				}
			} else {
				if ok, err := f.backend.JobWorkflow(t.Context(), f.key, group.Envelope()); err != nil || !ok {
					t.Fatal(ok, err)
				}
				if len(f.quarantine(t).Records) != 2 {
					t.Fatal("completed proof did not extend")
				}
			}
		})
	}
}
