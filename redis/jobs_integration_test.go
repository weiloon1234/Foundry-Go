package redis

import (
	"encoding/json"
	"errors"
	driver "github.com/redis/go-redis/v9"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jobtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func TestRedisScheduledInstantNeverRoundsEarly(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		name := "standalone"
		if workflow {
			name = "workflow"
		}
		t.Run(name, func(t *testing.T) {
			client, backend, key := jobFixture(t)
			at := time.Now().Truncate(time.Second).Add(time.Minute + 123456*time.Nanosecond)
			d := jobs.Define[jobtest.Payload]("scheduled.boundary", 1, jobs.DefaultPolicy(key.Queue()))
			p, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{At: at})
			if err != nil {
				t.Fatal(err)
			}
			if workflow {
				group, err := jobs.NewChain(p.Step())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := backend.JobWorkflow(t.Context(), key, group.Envelope()); err != nil {
					t.Fatal(err)
				}
			} else if _, err := backend.JobEnqueue(t.Context(), key, p.Envelope()); err != nil {
				t.Fatal(err)
			}
			owner, err := lease.NewOwner()
			if err != nil {
				t.Fatal(err)
			}
			for offset := range int64(2) {
				if err := client.raw.HSet(t.Context(), jobKeys(key)[4], "time", at.UnixMilli()+offset).Err(); err != nil {
					t.Fatal(err)
				}
				found, err := backend.JobReserve(t.Context(), key, owner, time.Minute)
				if err != nil || found.IsSet() != (offset == 1) {
					t.Fatal("scheduled eligibility rounded early", err)
				}
			}
		})
	}
}

func TestRedisRetiredWorkflowReusesFullCapacity(t *testing.T) {
	client, _, key := jobFixture(t)
	config := jobs.DefaultQueueConfig()
	config.MaxEntries = 1
	backend, err := NewJobBackend(client, config)
	if err != nil {
		t.Fatal(err)
	}
	d := jobs.Define[jobtest.Payload]("one.step", 1, jobs.DefaultPolicy(key.Queue()))
	pending, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	group, err := jobs.NewChain(pending.Step())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobWorkflow(t.Context(), key, group.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := backend.JobCancelWorkflow(t.Context(), key, group.ID()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	metadata := jobKeys(key)[4]
	now, err := client.raw.HGet(t.Context(), metadata, "time").Int64()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.raw.HSet(t.Context(), metadata, "time", now+config.Retention.Milliseconds()).Err(); err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobWorkflow(t.Context(), key, group.Envelope()); err != nil || !ok {
		t.Fatal("retired workflow still consumes capacity", err)
	}
}

func TestRedisCorruptJobCountersDoNotMutateAnyQueueKey(t *testing.T) {
	for _, field := range []string{"attempts", "maximum", "available", "live"} {
		t.Run(field, func(t *testing.T) {
			client, backend, key := jobFixture(t)
			d := jobs.Define[jobtest.Payload]("corrupt.counter", 1, jobs.DefaultPolicy(key.Queue()))
			pending, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.JobEnqueue(t.Context(), key, pending.Envelope()); err != nil {
				t.Fatal(err)
			}
			keys := jobKeys(key)
			if field == "live" {
				if err := client.raw.HSet(t.Context(), keys[4], field, "0.5").Err(); err != nil {
					t.Fatal(err)
				}
			} else {
				data, err := client.raw.HGet(t.Context(), keys[0], pending.Envelope().ID().String()).Bytes()
				if err != nil {
					t.Fatal(err)
				}
				var row map[string]json.RawMessage
				if err := json.Unmarshal(data, &row); err != nil {
					t.Fatal(err)
				}
				row[field] = json.RawMessage("0.5")
				data, err = json.Marshal(row)
				if err != nil {
					t.Fatal(err)
				}
				if err := client.raw.HSet(t.Context(), keys[0], pending.Envelope().ID().String(), data).Err(); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() map[string]string {
				result := make(map[string]string)
				for _, key := range keys {
					data, err := client.raw.Dump(t.Context(), key).Result()
					if err != nil && !errors.Is(err, driver.Nil) {
						t.Fatal(err)
					}
					result[key] = data
				}
				return result
			}
			before := snapshot()
			if _, err := backend.JobCancel(t.Context(), key, pending.Envelope().Target()); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
			if !maps.Equal(before, snapshot()) {
				t.Fatal("corrupt counters allowed partial queue mutation")
			}
		})
	}
}

func jobFixture(t *testing.T) (*Client, *JobBackend, jobs.Key) {
	t.Helper()
	client, namespace, track := integrationAddresses(t, nil)
	key, err := jobs.NewKey(namespace, "contract")
	if err != nil {
		t.Fatal(err)
	}
	for _, physical := range jobKeys(key) {
		track(physical)
	}
	backend, err := NewJobBackend(client, jobs.DefaultQueueConfig())
	if err != nil {
		t.Fatal(err)
	}
	return client, backend, key
}
func TestRedisSharedJobContract(t *testing.T) {
	jobtest.Run(t, func(t *testing.T) jobtest.Fixture {
		client, backend, key := jobFixture(t)
		return jobtest.Fixture{Backend: backend, Key: key, Advance: func(d time.Duration) {
			now, err := client.raw.HGet(t.Context(), jobKeys(key)[4], "time").Int64()
			if err != nil {
				t.Fatal(err)
			}
			if err := client.raw.HSet(t.Context(), jobKeys(key)[4], "time", now+d.Milliseconds()).Err(); err != nil {
				t.Fatal(err)
			}
		}, Expire: func(reservation jobs.Reservation) {
			keys := jobKeys(key)
			raw, err := client.raw.HGet(t.Context(), keys[0], reservation.Envelope.ID().String()).Result()
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatal(err)
			}
			record["expiry"] = json.RawMessage("1")
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.raw.HSet(t.Context(), keys[0], reservation.Envelope.ID().String(), data).Err(); err != nil {
				t.Fatal(err)
			}
			if err := client.raw.ZAdd(t.Context(), keys[2], driver.Z{Score: 1, Member: reservation.Envelope.ID().String()}).Err(); err != nil {
				t.Fatal(err)
			}
		}}
	})
}
func TestRedisJobPolicyConflictAndCorruptionDoNotMutate(t *testing.T) {
	client, backend, key := jobFixture(t)
	definition := jobs.Define[jobtest.Payload]("job.policy", 1, jobs.DefaultPolicy(key.Queue()))
	pending, err := definition.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(t.Context(), key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultQueueConfig()
	config.MaxHistory++
	conflicting, err := NewJobBackend(client, config)
	if err != nil {
		t.Fatal(err)
	}
	before := client.raw.Dump(t.Context(), jobKeys(key)[0]).Val()
	// A policy mismatch is a typed operator problem: neither an overload nor an
	// invalid job (the outbox must keep retrying such rows).
	if _, err := conflicting.JobEnqueue(t.Context(), key, pending.Envelope()); !errors.Is(err, jobs.ErrQueuePolicy) || errors.Is(err, fault.Overloaded) || errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if client.raw.Dump(t.Context(), jobKeys(key)[0]).Val() != before {
		t.Fatal("policy conflict mutated records")
	}
	if err := client.raw.HSet(t.Context(), jobKeys(key)[0], pending.Envelope().ID().String(), "corrupt").Err(); err != nil {
		t.Fatal(err)
	}
	before = client.raw.Dump(t.Context(), jobKeys(key)[0]).Val()
	if _, err := backend.JobInspect(t.Context(), key, pending.Envelope().ID()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if client.raw.Dump(t.Context(), jobKeys(key)[0]).Val() != before {
		t.Fatal("corruption was overwritten")
	}
}

func TestRedisQueueCapacityCountsOnlyLiveWorkAndEvictsOldestTerminal(t *testing.T) {
	client, _, key := jobFixture(t)
	config := jobs.DefaultQueueConfig()
	config.MaxEntries, config.MaxRetained = 1, 1
	backend, err := NewJobBackend(client, config)
	if err != nil {
		t.Fatal(err)
	}
	d := jobs.Define[jobtest.Payload]("capacity.work", 1, jobs.DefaultPolicy(key.Queue()))
	var finished []jobs.ExecutionID
	for range 2 {
		pending, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := backend.JobEnqueue(t.Context(), key, pending.Envelope()); err != nil || !ok {
			t.Fatal("retained terminal record blocked enqueue", ok, err)
		}
		owner, err := lease.NewOwner()
		if err != nil {
			t.Fatal(err)
		}
		found, err := backend.JobReserve(t.Context(), key, owner, time.Minute)
		reservation, ok := found.Get()
		if err != nil || !ok {
			t.Fatal(err)
		}
		if _, err := backend.JobStart(t.Context(), key, reservation.Ownership); err != nil {
			t.Fatal(err)
		}
		if ok, err := backend.JobFinish(t.Context(), key, reservation.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		finished = append(finished, pending.Envelope().ID())
	}
	// The next command evicts the oldest terminal record beyond MaxRetained.
	live, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobEnqueue(t.Context(), key, live.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if found, err := backend.JobInspect(t.Context(), key, finished[0]); err != nil || found.IsSet() {
		t.Fatal("oldest terminal record was not evicted", err)
	}
	if found, err := backend.JobInspect(t.Context(), key, finished[1]); err != nil || !found.IsSet() {
		t.Fatal("newest terminal record was evicted", err)
	}
	extra, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(t.Context(), key, extra.Envelope()); !errors.Is(err, jobs.ErrQueueFull) || !errors.Is(err, fault.Overloaded) {
		t.Fatal("live capacity was not enforced as overload", err)
	}
}

func TestRedisJobLayoutSeparatesEnvelopeAndSkipsNoopWrites(t *testing.T) {
	client, backend, key := jobFixture(t)
	d := jobs.Define[jobtest.Payload]("layout.work", 1, jobs.DefaultPolicy(key.Queue()))
	pending, err := d.Capture(t.Context(), jobtest.Payload{Text: "envelope"}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(t.Context(), key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	keys := jobKeys(key)
	id := pending.Envelope().ID().String()
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(client.raw.HGet(t.Context(), keys[0], id).Val()), &row); err != nil {
		t.Fatal(err)
	}
	if _, embedded := row["envelope"]; embedded || client.raw.HExists(t.Context(), keys[9], id).Val() != true {
		t.Fatal("mutable record still embeds its immutable envelope")
	}
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := backend.JobReserve(t.Context(), key, owner, time.Minute)
	reservation, ok := found.Get()
	if err != nil || !ok || reservation.Envelope.PayloadJSON() != pending.Envelope().PayloadJSON() {
		t.Fatal("reservation did not restore the envelope", err)
	}
	before := client.raw.Dump(t.Context(), keys[4]).Val()
	if found, err := backend.JobReserve(t.Context(), key, owner, time.Minute); err != nil || found.IsSet() {
		t.Fatal(err)
	}
	if client.raw.Dump(t.Context(), keys[4]).Val() != before {
		t.Fatal("empty reservation rewrote queue metadata")
	}
}

func TestRedisJobLayoutOneQueueMigratesOnlyExplicitly(t *testing.T) {
	client, backend, key := jobFixture(t)
	d := jobs.Define[jobtest.Payload]("legacy.work", 1, jobs.DefaultPolicy(key.Queue()))
	pending, err := d.Capture(t.Context(), jobtest.Payload{Text: "legacy"}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(t.Context(), key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	// Rewrite the queue as layout 1: embedded envelope, count/bytes counters and
	// the former policy identity, as the previous release stores it.
	keys := jobKeys(key)
	id := pending.Envelope().ID().String()
	envelope := client.raw.HGet(t.Context(), keys[9], id).Val()
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(client.raw.HGet(t.Context(), keys[0], id).Val()), &row); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	row["envelope"] = encoded
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	bytes := client.raw.HGet(t.Context(), keys[4], "live_bytes").Val()
	pipe := client.raw.TxPipeline()
	pipe.HSet(t.Context(), keys[0], id, data)
	pipe.Del(t.Context(), keys[9])
	pipe.HDel(t.Context(), keys[4], "layout", "live", "live_bytes", "retained", "retained_bytes", "failed")
	pipe.HSet(t.Context(), keys[4], "config", backend.legacy, "count", 1, "bytes", bytes)
	if _, err := pipe.Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		result := make(map[string]string)
		for _, physical := range keys {
			data, err := client.raw.Dump(t.Context(), physical).Result()
			if err != nil && !errors.Is(err, driver.Nil) {
				t.Fatal(err)
			}
			result[physical] = data
		}
		return result
	}
	before := snapshot()
	// Implicit use never migrates: the previous release keeps working.
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobReserve(t.Context(), key, owner, time.Minute); !errors.Is(err, jobs.ErrLegacyLayout) {
		t.Fatal("layout-1 queue was used without an explicit migration", err)
	}
	other, err := d.Capture(t.Context(), jobtest.Payload{Text: "new"}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(t.Context(), key, other.Envelope()); !errors.Is(err, jobs.ErrLegacyLayout) || errors.Is(err, fault.Conflict) {
		t.Fatal("legacy layout was not reported as such", err)
	}
	if !maps.Equal(before, snapshot()) {
		t.Fatal("implicit use changed a layout-1 queue")
	}
	// The explicit migration preserves records and is idempotent.
	if migrated, err := backend.JobMigrateLayout(t.Context(), key); err != nil || !migrated {
		t.Fatal("explicit migration", migrated, err)
	}
	if migrated, err := backend.JobMigrateLayout(t.Context(), key); err != nil || migrated {
		t.Fatal("repeated migration changed the queue", migrated, err)
	}
	if client.raw.HGet(t.Context(), keys[4], "layout").Val() != "2" || client.raw.HGet(t.Context(), keys[4], "live").Val() != "1" {
		t.Fatal("queue was not migrated to layout 2")
	}
	found, err := backend.JobReserve(t.Context(), key, owner, time.Minute)
	reservation, ok := found.Get()
	if err != nil || !ok || reservation.Envelope.PayloadJSON() != pending.Envelope().PayloadJSON() || reservation.Envelope.ID() != pending.Envelope().ID() {
		t.Fatal("migrated record was not preserved", err)
	}
	if !client.raw.HExists(t.Context(), keys[9], id).Val() {
		t.Fatal("record did not move its envelope on its next write")
	}
}

// TestRedisLayoutMigrationCountsDecodedRecordState migrates a layout-1 queue
// holding a retried-then-succeeded record (its history contains a failed
// transition), a failed record, live work and a finished workflow group. The
// migrated counters must equal the layout-2 accounting of the same records.
func TestRedisLayoutMigrationCountsDecodedRecordState(t *testing.T) {
	client, backend, key := jobFixture(t)
	ctx := t.Context()
	d := jobs.Define[jobtest.Payload]("legacy.mixed", 1, jobs.DefaultPolicy(key.Queue()))
	capture := func(text string) jobs.Envelope {
		t.Helper()
		pending, err := d.Capture(ctx, jobtest.Payload{Text: text}, jobs.Options[jobtest.Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		return pending.Envelope()
	}
	finish := func(want jobs.ExecutionID, state jobs.State) {
		t.Helper()
		owner, err := lease.NewOwner()
		if err != nil {
			t.Fatal(err)
		}
		found, err := backend.JobReserve(ctx, key, owner, time.Minute)
		claim, ok := found.Get()
		if err != nil || !ok || claim.Envelope.ID() != want {
			t.Fatal("expected reservation", err)
		}
		if _, err := backend.JobStart(ctx, key, claim.Ownership); err != nil {
			t.Fatal(err)
		}
		result := jobs.Result{State: state}
		if state == jobs.Failed {
			result.Reason = jobs.HandlerFailed
		}
		if changed, err := backend.JobFinish(ctx, key, claim.Ownership, result); err != nil || !changed {
			t.Fatal(changed, err)
		}
	}
	retried, failed, waiting := capture("retried"), capture("failed"), capture("waiting")
	if _, err := backend.JobEnqueue(ctx, key, retried); err != nil {
		t.Fatal(err)
	}
	finish(retried.ID(), jobs.Failed)
	found, err := backend.JobInspect(ctx, key, retried.ID())
	record, _ := found.Get()
	token, tokenErr := record.RetryToken()
	if err != nil || tokenErr != nil {
		t.Fatal(err, tokenErr)
	}
	if changed, err := backend.JobRetry(ctx, key, jobs.RetryRequest{Target: retried.Target(), Token: token}); err != nil || !changed {
		t.Fatal("manual retry", changed, err)
	}
	finish(retried.ID(), jobs.Succeeded)
	if _, err := backend.JobEnqueue(ctx, key, failed); err != nil {
		t.Fatal(err)
	}
	finish(failed.ID(), jobs.Failed)
	pending, err := d.Capture(ctx, jobtest.Payload{Text: "workflow"}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	group, err := jobs.NewChain(pending.Step())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobWorkflow(ctx, key, group.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := backend.JobCancelWorkflow(ctx, key, group.ID()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := backend.JobEnqueue(ctx, key, waiting); err != nil {
		t.Fatal(err)
	}
	keys := jobKeys(key)
	counters := []string{"live", "live_bytes", "retained", "retained_bytes", "failed"}
	read := func() []string {
		values, err := client.raw.HMGet(ctx, keys[4], counters...).Result()
		if err != nil {
			t.Fatal(err)
		}
		result := make([]string, len(values))
		for i, value := range values {
			result[i], _ = value.(string)
		}
		return result
	}
	expected := read()
	if expected[4] != "1" || expected[2] != "3" {
		t.Fatal("unexpected layout-2 accounting", expected)
	}
	// Rewrite every record as layout 1: embedded envelope and one count/bytes
	// pair, as the previous release stores the queue.
	rows, err := client.raw.HGetAll(ctx, keys[0]).Result()
	if err != nil {
		t.Fatal(err)
	}
	envelopes, err := client.raw.HGetAll(ctx, keys[9]).Result()
	if err != nil {
		t.Fatal(err)
	}
	pipe := client.raw.TxPipeline()
	for id, text := range rows {
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &row); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(envelopes[id])
		if err != nil {
			t.Fatal(err)
		}
		row["envelope"] = encoded
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		pipe.HSet(ctx, keys[0], id, data)
	}
	sum := func(fields ...string) int64 {
		t.Helper()
		var total int64
		for _, field := range fields {
			n, err := client.raw.HGet(ctx, keys[4], field).Int64()
			if err != nil {
				t.Fatal(err)
			}
			total += n
		}
		return total
	}
	count, bytes := sum("live", "retained"), sum("live_bytes", "retained_bytes")
	pipe.Del(ctx, keys[9])
	pipe.HDel(ctx, keys[4], append([]string{"layout"}, counters...)...)
	pipe.HSet(ctx, keys[4], "config", backend.legacy, "count", count, "bytes", bytes)
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if migrated, err := backend.JobMigrateLayout(ctx, key); err != nil || !migrated {
		t.Fatal("explicit migration", migrated, err)
	}
	if got := read(); !slices.Equal(got, expected) {
		t.Fatal("migrated counters differ from the records' accounting", got, expected)
	}
	stats, err := backend.JobStats(ctx, key)
	if err != nil || stats.Failed != 1 || stats.Retained != 3 || stats.Waiting != 1 {
		t.Fatalf("migrated stats: %+v %v", stats, err)
	}
	// The migrated queue keeps working: the live record completes.
	finish(waiting.ID(), jobs.Succeeded)
}

// A finished workflow's group bytes move from live to retained accounting and
// leave it when the group is retired.
func TestRedisFinishedWorkflowBytesAreRetainedNotLive(t *testing.T) {
	client, backend, key := jobFixture(t)
	ctx := t.Context()
	d := jobs.Define[jobtest.Payload]("group.bytes", 1, jobs.DefaultPolicy(key.Queue()))
	pending, err := d.Capture(ctx, jobtest.Payload{Text: "step"}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	group, err := jobs.NewChain(pending.Step())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobWorkflow(ctx, key, group.Envelope()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := backend.JobReserve(ctx, key, owner, time.Minute)
	claim, ok := found.Get()
	if err != nil || !ok {
		t.Fatal("expected reservation", err)
	}
	if _, err := backend.JobStart(ctx, key, claim.Ownership); err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobFinish(ctx, key, claim.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	metadata := jobKeys(key)[4]
	counter := func(field string) int64 {
		t.Helper()
		n, err := client.raw.HGet(ctx, metadata, field).Int64()
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if counter("live") != 0 || counter("live_bytes") != 0 || counter("retained") != 1 {
		t.Fatal("finished workflow bytes stayed live", counter("live_bytes"))
	}
	record, err := client.raw.HGet(ctx, jobKeys(key)[0], claim.Envelope.ID().String()).Result()
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Bytes int64 `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(record), &stored); err != nil {
		t.Fatal(err)
	}
	if counter("retained_bytes") <= stored.Bytes {
		t.Fatal("group bytes were not retained", counter("retained_bytes"), stored.Bytes)
	}
	// Retiring the group releases both its record and group bytes.
	now := counter("time")
	if err := client.raw.HSet(ctx, metadata, "time", now+jobs.DefaultQueueConfig().Retention.Milliseconds()).Err(); err != nil {
		t.Fatal(err)
	}
	if ok, err := backend.JobWorkflow(ctx, key, group.Envelope()); err != nil || !ok {
		t.Fatal("retired workflow was not reusable", ok, err)
	}
	if counter("retained") != 0 || counter("retained_bytes") != 0 {
		t.Fatal("retired group bytes were not released", counter("retained_bytes"))
	}
}

// An enqueue wakes only workers of its own queue.
func TestRedisWakeupsArePerQueue(t *testing.T) {
	_, backend, key := jobFixture(t)
	_, _, other := jobFixture(t)
	mine, theirs := backend.JobWakeup(key), backend.JobWakeup(other)
	d := jobs.Define[jobtest.Payload]("wake.work", 1, jobs.DefaultPolicy(key.Queue()))
	pending, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(t.Context(), key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-mine:
	default:
		t.Fatal("enqueue did not wake its own queue")
	}
	select {
	case <-theirs:
		t.Fatal("enqueue woke another queue")
	default:
	}
}
