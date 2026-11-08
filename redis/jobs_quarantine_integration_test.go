package redis

import (
	"bytes"
	"encoding/json"
	"errors"
	jobcommand "github.com/weiloon1234/Foundry-Go/jobs/command"
	"io"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jobtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type quarantineFixture struct {
	client  *Client
	backend *JobBackend
	key     jobs.Key
	keys    []string
	owner   lease.Owner
}

func newQuarantineFixture(t *testing.T) quarantineFixture {
	t.Helper()
	client, backend, key := jobFixture(t)
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	return quarantineFixture{client, backend, key, jobKeys(key), owner}
}
func (f quarantineFixture) pending(t *testing.T) jobs.Pending[jobtest.Payload] {
	t.Helper()
	d := jobs.Define[jobtest.Payload]("quarantine.work", 1, jobs.DefaultPolicy(f.key.Queue()))
	p, err := d.Capture(t.Context(), jobtest.Payload{}, jobs.Options[jobtest.Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (f quarantineFixture) enqueue(t *testing.T) jobs.Envelope {
	t.Helper()
	e := f.pending(t).Envelope()
	if ok, err := f.backend.JobEnqueue(t.Context(), f.key, e); err != nil || !ok {
		t.Fatal(ok, err)
	}
	return e
}
func (f quarantineFixture) reserve(t *testing.T) jobs.Reservation {
	t.Helper()
	found, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := found.Get()
	if !ok {
		t.Fatal("expected reservation")
	}
	return r
}
func (f quarantineFixture) finish(t *testing.T, r jobs.Reservation, state jobs.State) {
	t.Helper()
	if _, err := f.backend.JobStart(t.Context(), f.key, r.Ownership); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.backend.JobFinish(t.Context(), f.key, r.Ownership, jobs.Result{State: state}); err != nil || !ok {
		t.Fatal(ok, err)
	}
}
func (f quarantineFixture) advance(t *testing.T, d time.Duration) {
	t.Helper()
	now, err := f.client.raw.HGet(t.Context(), f.keys[4], "time").Int64()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.client.raw.HSet(t.Context(), f.keys[4], "time", now+d.Milliseconds()).Err(); err != nil {
		t.Fatal(err)
	}
}
func (f quarantineFixture) counters(t *testing.T) map[string]string {
	t.Helper()
	fields, err := f.client.raw.HGetAll(t.Context(), f.keys[4]).Result()
	if err != nil {
		t.Fatal(err)
	}
	delete(fields, "time")
	return fields
}
func (f quarantineFixture) stored(t *testing.T, index int, id jobs.ExecutionID) string {
	t.Helper()
	s, err := f.client.raw.HGet(t.Context(), f.keys[index], id.String()).Result()
	if err != nil && !errors.Is(err, driver.Nil) {
		t.Fatal(err)
	}
	return s
}
func (f quarantineFixture) put(t *testing.T, id jobs.ExecutionID, text string) {
	t.Helper()
	if err := f.client.raw.HSet(t.Context(), f.keys[0], id.String(), text).Err(); err != nil {
		t.Fatal(err)
	}
}
func (f quarantineFixture) list(t *testing.T) jobs.Page {
	t.Helper()
	page, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: jobs.MaxListLimit})
	if err != nil {
		t.Fatal(err)
	}
	return page
}
func (f quarantineFixture) assertUnavailable(t *testing.T, e jobs.Envelope) {
	t.Helper()
	owned, err := jobs.NewOwnership(e.ID(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotJobKeys(t, f.client, f.keys)
	checks := []func() error{
		func() error { _, err := f.backend.JobInspect(t.Context(), f.key, e.ID()); return err },
		func() error { _, err := f.backend.JobCancel(t.Context(), f.key, e.Target()); return err },
		func() error { _, err := f.backend.JobRenew(t.Context(), f.key, owned, time.Minute); return err },
		func() error { _, err := f.backend.JobStart(t.Context(), f.key, owned); return err },
		func() error {
			_, err := f.backend.JobFinish(t.Context(), f.key, owned, jobs.Result{State: jobs.Succeeded})
			return err
		},
		func() error { _, err := f.backend.JobForget(t.Context(), f.key, e.Target()); return err },
		func() error {
			_, err := f.backend.JobRetry(t.Context(), f.key, jobs.RetryRequest{Target: e.Target(), Token: jobs.RetryToken(strings.Repeat("a", 64))})
			return err
		},
		func() error { _, err := f.backend.JobEnqueue(t.Context(), f.key, e); return err },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("operation %d: %v", i, err)
		}
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("failed direct operations changed queue state")
	}
}

func snapshotJobKeys(t *testing.T, client *Client, keys []string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, key := range keys {
		kind, err := client.raw.Type(t.Context(), key).Result()
		if err != nil {
			t.Fatal(err)
		}
		var data any
		switch kind {
		case "hash":
			data, err = client.raw.HGetAll(t.Context(), key).Result()
		case "zset":
			data, err = client.raw.ZRangeWithScores(t.Context(), key, 0, -1).Result()
		case "none":
		default:
			data, err = client.raw.Get(t.Context(), key).Result()
		}
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		result[key] = kind + ":" + string(encoded)
	}
	return result
}

func (f quarantineFixture) edit(t *testing.T, id jobs.ExecutionID, fields map[string]any) {
	t.Helper()
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(f.stored(t, 0, id)), &row); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if value == nil {
			delete(row, key)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		row[key] = encoded
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	f.put(t, id, string(encoded))
}

func (f quarantineFixture) at(t *testing.T, id jobs.ExecutionID, when int64) {
	t.Helper()
	f.edit(t, id, map[string]any{"available": when})
	if err := f.client.raw.ZAdd(t.Context(), f.keys[1], driver.Z{Score: float64(when), Member: id.String()}).Err(); err != nil {
		t.Fatal(err)
	}
}

func (f quarantineFixture) quarantine(t *testing.T) jobs.QuarantinePage {
	t.Helper()
	page, err := f.backend.JobQuarantined(t.Context(), f.key, jobs.QuarantineOptions{Limit: jobs.MaxListLimit})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func (f quarantineFixture) unchanged(t *testing.T, operation func() error) {
	t.Helper()
	before := snapshotJobKeys(t, f.client, f.keys)
	if err := operation(); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected invalid state: %v", err)
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("rejected operation changed queue")
	}
}

// R1 F2/F5/F6: rejected charges and envelopes never affect a healthy sibling.
func TestRedisQuarantineIndependentCorruptionPreservesAccounting(t *testing.T) {
	for _, damage := range []string{"json", "charge", "oversized", "envelope", "missing", "score"} {
		t.Run(damage, func(t *testing.T) {
			f := newQuarantineFixture(t)
			bad, good := f.enqueue(t), f.enqueue(t)
			f.at(t, bad.ID(), 1)
			switch damage {
			case "json":
				f.put(t, bad.ID(), "private damaged record")
			case "charge":
				f.edit(t, bad.ID(), map[string]any{"attempts": 0.5, "bytes": f.backend.config.MaxBytes})
			case "oversized":
				var limits struct {
					RecordBytes int `json:"record_bytes"`
				}
				if err := json.Unmarshal([]byte(f.backend.limits), &limits); err != nil {
					t.Fatal(err)
				}
				f.put(t, bad.ID(), `{"padding":"`+strings.Repeat("x", limits.RecordBytes)+`"}`)
			case "envelope":
				if err := f.client.raw.HDel(t.Context(), f.keys[9], bad.ID().String()).Err(); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := f.client.raw.HDel(t.Context(), f.keys[0], bad.ID().String()).Err(); err != nil {
					t.Fatal(err)
				}
			case "score":
				if err := f.client.raw.ZAdd(t.Context(), f.keys[1], driver.Z{Score: 0, Member: bad.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			raw, envelope, before := f.stored(t, 0, bad.ID()), f.stored(t, 9, bad.ID()), f.counters(t)
			claim := f.reserve(t)
			if claim.Envelope.ID() != good.ID() {
				t.Fatal("corruption stalled healthy work")
			}
			if !maps.Equal(before, f.counters(t)) {
				t.Fatal("quarantine refunded accounting")
			}
			page := f.quarantine(t)
			if len(page.Records) != 1 || page.Records[0].ID != bad.ID() || page.Records[0].QuarantinedAt.IsZero() {
				t.Fatal(page)
			}
			f.assertUnavailable(t, bad)
			f.finish(t, claim, jobs.Succeeded)
			if f.stored(t, 0, bad.ID()) != raw || f.stored(t, 9, bad.ID()) != envelope {
				t.Fatal("quarantine rewrote raw data")
			}
			if f.quarantine(t).Records[0] != page.Records[0] {
				t.Fatal("quarantine time changed")
			}
			for _, index := range []int{1, 2, 3} {
				if _, err := f.client.raw.ZScore(t.Context(), f.keys[index], bad.ID().String()).Result(); !errors.Is(err, driver.Nil) {
					t.Fatal("quarantined scheduling entry", index, err)
				}
			}
			if score, err := f.client.raw.ZScore(t.Context(), f.keys[8], bad.ID().String()).Result(); err != nil || score != 0 {
				t.Fatal("lost quarantined identity", err)
			}
		})
	}
}

// R1 F1: refusing A's deletion leaves its failed charge intact when B is removed.
func TestRedisQuarantineFailedChargeSurvivesHealthyForget(t *testing.T) {
	f := newQuarantineFixture(t)
	a := f.enqueue(t)
	f.finish(t, f.reserve(t), jobs.Failed)
	b := f.enqueue(t)
	f.finish(t, f.reserve(t), jobs.Succeeded)
	f.put(t, a.ID(), "corrupt")
	f.list(t)
	f.assertUnavailable(t, a)
	if changed, err := f.backend.JobForget(t.Context(), f.key, b.Target()); err != nil || !changed {
		t.Fatal(changed, err)
	}
	stats, err := f.backend.JobStats(t.Context(), f.key)
	if err != nil || stats.Retained != 1 || stats.Failed != 1 || stats.Quarantined != 1 {
		t.Fatal(stats, err)
	}
	if got, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil || got.IsSet() {
		t.Fatal(err)
	}
}

// R2 F2: no failed flag is recovered from invalid JSON, for any healthy removal path.
func TestRedisQuarantineForgedFailedFlagDoesNotConsumeSiblingCount(t *testing.T) {
	for _, action := range []string{"retry", "forget", "retention", "eviction"} {
		t.Run(action, func(t *testing.T) {
			f := newQuarantineFixture(t)
			if action == "eviction" {
				config := f.backend.config
				config.MaxRetained = 2
				var err error
				f.backend, err = NewJobBackend(f.client, config)
				if err != nil {
					t.Fatal(err)
				}
			}
			a := f.enqueue(t)
			f.finish(t, f.reserve(t), jobs.Succeeded)
			b := f.enqueue(t)
			f.finish(t, f.reserve(t), jobs.Failed)
			found, err := f.backend.JobInspect(t.Context(), f.key, b.ID())
			if err != nil {
				t.Fatal(err)
			}
			record, _ := found.Get()
			token, err := record.RetryToken()
			if err != nil {
				t.Fatal(err)
			}
			f.put(t, a.ID(), `{"state":"failed"}`)
			f.list(t)
			f.assertUnavailable(t, a)
			switch action {
			case "retry":
				if ok, err := f.backend.JobRetry(t.Context(), f.key, jobs.RetryRequest{Target: b.Target(), Token: token}); err != nil || !ok {
					t.Fatal(ok, err)
				}
				f.finish(t, f.reserve(t), jobs.Succeeded)
			case "forget":
				if ok, err := f.backend.JobForget(t.Context(), f.key, b.Target()); err != nil || !ok {
					t.Fatal(ok, err)
				}
			case "retention":
				f.advance(t, f.backend.config.Retention+time.Second)
			case "eviction":
				f.enqueue(t)
				f.finish(t, f.reserve(t), jobs.Succeeded)
			}
			stats, err := f.backend.JobStats(t.Context(), f.key)
			if err != nil || stats.Failed != 0 || stats.Quarantined != 1 {
				t.Fatal(stats, err)
			}
		})
	}
}

// R1 F6 and R3 F5: both direct mutation and indexed reads use the same validator.
func TestRedisQuarantineHistoryAndEnvelopeValidation(t *testing.T) {
	badHistory := []string{`[42]`, `{}`, `[]`, `null`, `{"1":{"state":"waiting"}}`, `[null]`, `[[]]`,
		`[{"state":false,"reason":"","at":1,"attempt":0}]`,
		`[{"state":"waiting","reason":42,"at":1,"attempt":0}]`,
		`[{"state":"waiting","reason":"","at":1.5,"attempt":0}]`,
		`[{"state":"waiting","reason":"","at":253402300800000,"attempt":0}]`,
		`[{"state":"waiting","reason":"","at":1,"attempt":-1}]`,
		`[{"state":"waiting","reason":"","at":1,"attempt":4294967296}]`,
		`[{"state":"waiting","reason":"","at":1,"attempt":0,"retry":false}]`,
		`[{"state":"waiting","reason":"","at":1,"attempt":0,"retry":1001}]`,
		`[{"state":"waiting","at":1,"attempt":0}]`}
	for i, history := range badHistory {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			f := newQuarantineFixture(t)
			e := f.enqueue(t)
			f.edit(t, e.ID(), map[string]any{"history": json.RawMessage(history)})
			f.assertUnavailable(t, e)
			f.list(t)
			if len(f.quarantine(t).Records) != 1 {
				t.Fatal("history escaped indexed validation")
			}
		})
	}
	for _, field := range []string{"workflow", "position", "retries", "last_retry", "missing-envelope"} {
		t.Run(field, func(t *testing.T) {
			f := newQuarantineFixture(t)
			e := f.enqueue(t)
			claim := f.reserve(t)
			if _, err := f.backend.JobStart(t.Context(), f.key, claim.Ownership); err != nil {
				t.Fatal(err)
			}
			if field == "missing-envelope" {
				if err := f.client.raw.HDel(t.Context(), f.keys[9], e.ID().String()).Err(); err != nil {
					t.Fatal(err)
				}
			} else {
				f.edit(t, e.ID(), map[string]any{field: false})
			}
			f.assertUnavailable(t, e)
			if field == "workflow" {
				f.unchanged(t, func() error { _, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 10}); return err })
			} else {
				f.list(t)
				if len(f.quarantine(t).Records) != 1 {
					t.Fatal("not quarantined")
				}
			}
		})
	}
	t.Run("legacy-retry-omitted", func(t *testing.T) {
		f := newQuarantineFixture(t)
		e := f.enqueue(t)
		f.edit(t, e.ID(), map[string]any{"history": json.RawMessage(`[{"state":"waiting","reason":"","at":1,"attempt":4}]`)})
		if got := f.list(t); len(got.Records) != 1 || got.Records[0].History[0].Attempt != 4 {
			t.Fatal(got)
		}
	})
}

// R3 F4: a reclaimed lease cannot jump ready work beyond a full corrupt window.
func TestRedisQuarantineReserveExhaustionKeepsOrdering(t *testing.T) {
	for _, count := range []int{127, 128, 129} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := newQuarantineFixture(t)
			b := f.enqueue(t)
			claim := f.reserve(t)
			bad := make([]jobs.Envelope, count)
			for i := range bad {
				bad[i] = f.enqueue(t)
				f.at(t, bad[i].ID(), 1)
			}
			a := f.enqueue(t)
			f.at(t, a.ID(), 2)
			for _, e := range bad {
				f.put(t, e.ID(), "corrupt")
			}
			f.advance(t, time.Minute+time.Second)
			first, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if count >= 128 && first.IsSet() {
				t.Fatal("reserved beyond unproved window")
			}
			if count == 127 {
				r, _ := first.Get()
				if r.Envelope.ID() != a.ID() {
					t.Fatal("wrong earliest candidate")
				}
			} else {
				if got := f.reserve(t); got.Envelope.ID() != a.ID() {
					t.Fatal("reclaimed lease jumped ready work")
				}
			}
			if got := f.reserve(t); got.Envelope.ID() != b.ID() {
				t.Fatal("lease was not reclaimed", claim)
			}
		})
	}
}

func TestRedisQuarantineRollsBackWithInvalidDirectTarget(t *testing.T) {
	f := newQuarantineFixture(t)
	a := f.enqueue(t)
	f.finish(t, f.reserve(t), jobs.Succeeded)
	b := f.enqueue(t)
	f.at(t, b.ID(), maxJobInteger-1000)
	f.put(t, a.ID(), "corrupt retained")
	f.put(t, b.ID(), "corrupt target")
	f.advance(t, f.backend.config.Retention+time.Second)
	f.unchanged(t, func() error { _, err := f.backend.JobCancel(t.Context(), f.key, b.Target()); return err })
	if len(f.quarantine(t).Records) != 0 {
		t.Fatal("aborted maintenance persisted quarantine")
	}
	if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	if len(f.quarantine(t).Records) != 1 {
		t.Fatal("successful maintenance did not commit quarantine")
	}
}

func TestRedisQuarantineLeaseRetentionAndMarkerIdempotence(t *testing.T) {
	for _, source := range []string{"lease", "retention", "eviction"} {
		t.Run(source, func(t *testing.T) {
			f := newQuarantineFixture(t)
			if source == "eviction" {
				config := f.backend.config
				config.MaxRetained = 1
				var err error
				f.backend, err = NewJobBackend(f.client, config)
				if err != nil {
					t.Fatal(err)
				}
			}
			bad := f.enqueue(t)
			r := f.reserve(t)
			if source != "lease" {
				f.finish(t, r, jobs.Failed)
			}
			f.put(t, bad.ID(), "corrupt")
			before := f.counters(t)
			switch source {
			case "lease":
				f.advance(t, time.Minute+time.Second)
			case "retention":
				f.advance(t, f.backend.config.Retention+time.Second)
			case "eviction":
				f.enqueue(t)
				f.finish(t, f.reserve(t), jobs.Succeeded)
			}
			if _, err := f.backend.JobStats(t.Context(), f.key); err != nil {
				t.Fatal(err)
			}
			page := f.quarantine(t)
			if len(page.Records) != 1 {
				t.Fatal(page)
			}
			if source != "eviction" && !maps.Equal(before, f.counters(t)) {
				t.Fatal("quarantine altered charges")
			}
			for _, index := range []int{1, 2, 3} {
				if err := f.client.raw.ZAdd(t.Context(), f.keys[index], driver.Z{Score: 1, Member: bad.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute); err != nil {
				t.Fatal(err)
			}
			if f.quarantine(t).Records[0] != page.Records[0] {
				t.Fatal("marker timestamp changed")
			}
		})
	}
}

func TestRedisQuarantineMetadataSurvivesWorkflowFailureAndCursorRemoval(t *testing.T) {
	f := newQuarantineFixture(t)
	good := make([]jobs.Envelope, 129)
	for i := range good {
		good[i] = f.enqueue(t)
	}
	// Sort by the actual identity index, rather than assuming UUID creation order.
	ids, err := f.client.raw.ZRange(t.Context(), f.keys[8], 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	last := ids[len(ids)-1]
	if err := f.client.raw.HSet(t.Context(), f.keys[0], last, "private corruption").Err(); err != nil {
		t.Fatal(err)
	}
	var after jobs.ExecutionID
	for {
		page, err := f.backend.JobList(t.Context(), f.key, jobs.ListOptions{Limit: 100, After: after})
		if err != nil {
			t.Fatal(err)
		}
		if page.Next.IsZero() {
			break
		}
		after = page.Next
	}
	first := f.quarantine(t)
	if len(first.Records) != 0 || first.Next.IsZero() {
		t.Fatal("missing empty continuation page", first)
	}
	// Removal of the preceding identity must not invalidate its cursor.
	id := first.Next
	for _, e := range good {
		if e.ID() == id {
			if _, err := f.backend.JobCancel(t.Context(), f.key, e.Target()); err != nil {
				t.Fatal(err)
			}
			if _, err := f.backend.JobForget(t.Context(), f.key, e.Target()); err != nil {
				t.Fatal(err)
			}
		}
	}
	page, err := f.backend.JobQuarantined(t.Context(), f.key, jobs.QuarantineOptions{Limit: 100, After: id})
	if err != nil || len(page.Records) != 1 || page.Records[0].ID.String() != last {
		t.Fatal(page, err)
	}
	group := f.workflow(t, false, 2, false)
	a := group.Envelope().Steps()[0]
	// A due malformed workflow lease blocks ordinary maintenance, not metadata reads.
	f.edit(t, a.ID(), map[string]any{"state": "running", "owner": strings.Repeat("a", 64), "expiry": 1, "attempts": 0.5})
	if err := f.client.raw.ZRem(t.Context(), f.keys[1], a.ID().String()).Err(); err != nil {
		t.Fatal(err)
	}
	if err := f.client.raw.ZAdd(t.Context(), f.keys[2], driver.Z{Score: 1, Member: a.ID().String()}).Err(); err != nil {
		t.Fatal(err)
	}
	f.unchanged(t, func() error { _, err := f.backend.JobStats(t.Context(), f.key); return err })
	before := snapshotJobKeys(t, f.client, f.keys)
	page, err = f.backend.JobQuarantined(t.Context(), f.key, jobs.QuarantineOptions{Limit: 100, After: id})
	if err != nil || len(page.Records) != 1 || !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("metadata inspection ran maintenance", err)
	}
}

func TestRedisQuarantineCommandEmitsOnlyMetadata(t *testing.T) {
	f := newQuarantineFixture(t)
	for range 3 {
		e := f.enqueue(t)
		f.put(t, e.ID(), "private corrupt record")
	}
	f.list(t)
	registry, err := jobs.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(f.backend, registry, jobs.DefaultDispatchConfig(f.key.Namespace()))
	if err != nil {
		t.Fatal(err)
	}
	connection, err := jobs.NewConnection(dispatcher, f.key.Queue())
	if err != nil {
		t.Fatal(err)
	}
	connections, err := jobs.NewConnections("primary", jobs.NamedConnection{Name: "primary", Value: connection})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		c, err := jobcommand.Parse(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := c.Run(t.Context(), connections, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	raw := run("jobs", "quarantined", "--limit", "2", "--format", "json")
	var page jobs.QuarantinePage
	if err := json.Unmarshal([]byte(raw), &page); err != nil || len(page.Records) != 2 || page.Next.IsZero() {
		t.Fatal(page, err)
	}
	var fields struct {
		Records []map[string]json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	for _, row := range fields.Records {
		if len(row) != 2 || row["id"] == nil || row["quarantined_at"] == nil {
			t.Fatal("unexpected metadata fields")
		}
	}
	text := run("jobs", "quarantined", "--after", page.Next.String(), "--limit", "2")
	if strings.Contains(raw+text, "private") || strings.Contains(raw+text, "payload") || strings.Contains(text, "next:") {
		t.Fatal("unexpected command output")
	}
	stats := run("jobs", "stats")
	if !strings.Contains(stats, "quarantined=3") || !strings.Contains(stats, "blocked=3") {
		t.Fatal(stats)
	}
}

func TestRedisQuarantineExactlyFullReadyWindowDefersReclaimedLease(t *testing.T) {
	f := newQuarantineFixture(t)
	b := f.enqueue(t)
	f.reserve(t)
	for range 128 {
		e := f.enqueue(t)
		f.at(t, e.ID(), 1)
		f.put(t, e.ID(), "corrupt")
	}
	f.advance(t, time.Minute+time.Second)
	first, err := f.backend.JobReserve(t.Context(), f.key, f.owner, time.Minute)
	if err != nil || first.IsSet() {
		t.Fatal("full unproved window reserved reclaimed work", err)
	}
	if r := f.reserve(t); r.Envelope.ID() != b.ID() {
		t.Fatal("reclaimed lease disappeared")
	}
}

func TestRedisQuarantineReadyTieUsesIdentity(t *testing.T) {
	f := newQuarantineFixture(t)
	a, b := f.enqueue(t), f.enqueue(t)
	f.at(t, a.ID(), 1)
	f.at(t, b.ID(), 1)
	want := a.ID()
	if b.ID().String() < want.String() {
		want = b.ID()
	}
	if r := f.reserve(t); r.Envelope.ID() != want {
		t.Fatal("ready tie ignored identity")
	}
}

func TestRedisQuarantineFailedSiblingCanRetryAndAtomicRetryRejectsMarker(t *testing.T) {
	f := newQuarantineFixture(t)
	a := f.enqueue(t)
	f.finish(t, f.reserve(t), jobs.Failed)
	found, err := f.backend.JobInspect(t.Context(), f.key, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	original, _ := found.Get()
	oldToken, err := original.RetryToken()
	if err != nil {
		t.Fatal(err)
	}
	b := f.enqueue(t)
	f.finish(t, f.reserve(t), jobs.Failed)
	found, err = f.backend.JobInspect(t.Context(), f.key, b.ID())
	if err != nil {
		t.Fatal(err)
	}
	record, _ := found.Get()
	token, err := record.RetryToken()
	if err != nil {
		t.Fatal(err)
	}
	f.put(t, a.ID(), "corrupt failed A")
	f.list(t)
	encoded, err := a.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	f.unchanged(t, func() error {
		_, err := f.backend.command(t.Context(), f.key, jobRequest{Op: "retry", ID: a.ID().String(), Name: a.Name(), Version: a.Version(), RetryToken: oldToken, Envelope: string(encoded), ExpectedCreated: original.CreatedAt.UnixMilli(), ExpectedFinished: original.FinishedAt.UnixMilli(), ExpectedAttempts: original.Attempts, ExpectedRetries: original.Retries, MaxRetries: jobs.MaxManualRetries})
		return err
	})
	if changed, err := f.backend.JobRetry(t.Context(), f.key, jobs.RetryRequest{Target: b.Target(), Token: token}); err != nil || !changed {
		t.Fatal(changed, err)
	}
	f.finish(t, f.reserve(t), jobs.Succeeded)
	stats, err := f.backend.JobStats(t.Context(), f.key)
	if err != nil || stats.Failed != 1 || stats.Retained != 2 || stats.Quarantined != 1 {
		t.Fatal(stats, err)
	}
}

func TestRedisQuarantineIndexScoreValidation(t *testing.T) {
	for _, state := range []string{"lease", "finished", "identity", "delayed"} {
		t.Run(state, func(t *testing.T) {
			f := newQuarantineFixture(t)
			e := f.enqueue(t)
			index := 2
			switch state {
			case "lease":
				f.reserve(t)
			case "finished":
				f.finish(t, f.reserve(t), jobs.Succeeded)
				index = 3
			case "identity":
				index = 8
			case "delayed":
				f.at(t, e.ID(), maxJobInteger-1000)
				f.put(t, e.ID(), "corrupt delayed record")
			}
			if state != "delayed" {
				if err := f.client.raw.ZAdd(t.Context(), f.keys[index], driver.Z{Score: 1, Member: e.ID().String()}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			before := f.counters(t)
			f.list(t)
			if len(f.quarantine(t).Records) != 1 || !maps.Equal(before, f.counters(t)) {
				t.Fatal("inconsistent placement was not isolated")
			}
		})
	}
}

func TestRedisQuarantineEmptyInspectionDoesNotInitializeQueue(t *testing.T) {
	f := newQuarantineFixture(t)
	before := snapshotJobKeys(t, f.client, f.keys)
	if page := f.quarantine(t); len(page.Records) != 0 || !page.Next.IsZero() {
		t.Fatal(page)
	}
	if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) {
		t.Fatal("empty quarantine read wrote metadata")
	}
}
