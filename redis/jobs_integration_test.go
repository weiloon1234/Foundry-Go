package redis

import (
	"encoding/json"
	"errors"
	driver "github.com/redis/go-redis/v9"
	"maps"
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
	for _, field := range []string{"attempts", "maximum", "available", "count"} {
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
			if field == "count" {
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
	if _, err := conflicting.JobEnqueue(t.Context(), key, pending.Envelope()); !errors.Is(err, fault.Conflict) {
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
