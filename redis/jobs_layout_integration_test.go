package redis

import (
	"encoding/json"
	"errors"
	"maps"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func TestRedisJobUnsupportedLayoutsRejectWithoutWrites(t *testing.T) {
	for _, layout := range []string{"", "1", "2", "99", "foreign"} {
		t.Run(layout, func(t *testing.T) {
			f := newQuarantineFixture(t)
			f.enqueue(t)
			if err := f.client.raw.HSet(t.Context(), f.keys[4], "layout", layout).Err(); err != nil {
				t.Fatal(err)
			}
			before := snapshotJobKeys(t, f.client, f.keys)
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

func TestRedisJobRejectsTerminalWorkflowMemberInIndependentIndex(t *testing.T) {
	f := newQuarantineFixture(t)
	f.workflow(t, true, 2, false)
	claim := f.reserve(t)
	f.finish(t, claim, jobs.Succeeded)
	var row jobStoredRecord
	if err := json.Unmarshal([]byte(f.stored(t, 0, claim.Envelope.ID())), &row); err != nil {
		t.Fatal(err)
	}
	if err := f.client.raw.ZAdd(t.Context(), f.keys[3], driver.Z{Score: float64(row.Finished), Member: claim.Envelope.ID().String()}).Err(); err != nil {
		t.Fatal(err)
	}
	f.advance(t, f.backend.config.Retention+time.Second)
	before := snapshotJobKeys(t, f.client, f.keys)
	if _, err := f.backend.JobStats(t.Context(), f.key); !errors.Is(err, fault.Invalid) {
		t.Fatal("terminal workflow placement accepted", err)
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("rejected placement changed queue")
	}
}

func TestRedisJobWorkflowRequiresCallbackFields(t *testing.T) {
	for _, field := range []string{"catch", "finally"} {
		for _, value := range []string{"missing", "null", "false"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				f := newQuarantineFixture(t)
				group := f.workflow(t, false, 1, false)
				id := group.ID().String()
				raw, err := f.client.raw.HGet(t.Context(), f.keys[6], id).Result()
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]json.RawMessage
				if err := json.Unmarshal([]byte(raw), &wire); err != nil {
					t.Fatal(err)
				}
				if value == "missing" {
					delete(wire, field)
				} else {
					wire[field] = json.RawMessage(value)
				}
				encoded, err := json.Marshal(wire)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.client.raw.HSet(t.Context(), f.keys[6], id, encoded).Err(); err != nil {
					t.Fatal(err)
				}
				before := snapshotJobKeys(t, f.client, f.keys)
				if _, err := f.backend.JobWorkflowStatus(t.Context(), f.key, group.ID()); !errors.Is(err, fault.Invalid) {
					t.Fatal("invalid callback field accepted", err)
				}
				if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
					t.Fatal("invalid workflow changed queue")
				}
			})
		}
	}
}
