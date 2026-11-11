package httpcapture

import (
	"fmt"
	stdhttp "net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig() Config {
	c := DefaultConfig()
	c.BodyBytes, c.Rate, c.Burst = 1024, 10000, 10000
	return c
}

const inboundCost = int64(1024 + 1 + MetadataBytes)

func TestReservationBudgetCoversActiveQueuedAndDrained(t *testing.T) {
	c := testConfig()
	c.RetainedBytes = int64(c.BodyBytes + 1 + MetadataBytes)
	r, err := New[Snapshot](c)
	if err != nil {
		t.Fatal(err)
	}
	lane := r.NewLane()
	lease := lane.Reserve(inboundCost)
	if lease == nil || lane.Reserve(inboundCost) != nil {
		t.Fatal("active preread was not charged")
	}
	lease.Complete(Snapshot{Body: Body{Prefix: make([]byte, c.BodyBytes+1)}}, inboundCost)
	lease.Complete(Snapshot{}, 0)
	if lane.Reserve(inboundCost) != nil {
		t.Fatal("queue was not charged")
	}
	record := r.Drain()
	if record == nil || lane.Reserve(inboundCost) != nil {
		t.Fatal("consumer-held bytes were not charged")
	}
	r.Seal()
	select {
	case <-r.Done():
		t.Fatal("sealed before release")
	default:
	}
	record.Release()
	record.Release()
	if slots, bytes := r.Usage(); slots != 0 || bytes != 0 {
		t.Fatal(slots, bytes)
	}
	select {
	case <-r.Done():
	default:
		t.Fatal("did not drain")
	}
	if lane.Reserve(inboundCost) != nil || r.Drops()[DropBytes] != 3 || r.Drops()[DropSealed] != 1 {
		t.Fatal(r.Drops())
	}
}

func TestRecorderRateCapacityDropsAndShrink(t *testing.T) {
	c := testConfig()
	c.Rate, c.Burst, c.QueueSize = 1, 1, 2
	r, _ := New[Snapshot](c)
	lane := r.NewLane()
	now := time.Now()
	first := lane.reserve(now, inboundCost)
	if first == nil || lane.reserve(now, inboundCost) != nil {
		t.Fatal("rate admission")
	}
	first.Complete(Snapshot{}, MetadataBytes)
	if _, retained := r.Usage(); retained != MetadataBytes {
		t.Fatal(retained)
	}
	second := lane.reserve(now.Add(time.Second), inboundCost)
	if second == nil || lane.reserve(now.Add(2*time.Second), inboundCost) != nil {
		t.Fatal("capacity admission")
	}
	second.Complete(Snapshot{}, MetadataBytes)
	a, b := r.Drain(), r.Drain()
	if a.DroppedBefore != 1 || b.DroppedBefore != 1 || r.Drain() != nil {
		t.Fatal("loss was not carried exactly once")
	}
	a.Release()
	b.Release()
	if r.Drops()[DropRate] != 1 || r.Drops()[DropCapacity] != 1 {
		t.Fatal(r.Drops())
	}
	r.Seal()
}

func TestSealAllowsOutstandingCompletionAndReady(t *testing.T) {
	r, _ := New[Snapshot](testConfig())
	lane := r.NewLane()
	l := lane.Reserve(inboundCost)
	r.Seal()
	if lane.Reserve(inboundCost) != nil {
		t.Fatal("sealed admission")
	}
	if !l.Grow(1) {
		t.Fatal("sealing refused growth of an admitted capture")
	}
	l.Complete(Snapshot{}, MetadataBytes)
	select {
	case <-r.Ready():
	default:
		t.Fatal("consumer not notified")
	}
	r.Drain().Release()
	select {
	case <-r.Done():
	default:
		t.Fatal("outstanding reservation not drained")
	}
}

func TestRecorderConcurrentOwnership(t *testing.T) {
	r, _ := New[Snapshot](testConfig())
	lane := r.NewLane()
	var wg sync.WaitGroup
	for range 128 {
		wg.Go(func() {
			if lease := lane.Reserve(inboundCost); lease != nil {
				lease.Complete(Snapshot{Body: Body{Prefix: []byte("private")}}, MetadataBytes)
			}
		})
	}
	wg.Wait()
	r.Seal()
	count := 0
	for record := r.Drain(); record != nil; record = r.Drain() {
		record.Release()
		count++
	}
	if count != 128 {
		t.Fatal(count)
	}
	if slots, bytes := r.Usage(); slots != 0 || bytes != 0 {
		t.Fatal(slots, bytes)
	}
}

func TestRecorderBoundsAndDiagnostics(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.BodyBytes = 1023 }, func(c *Config) { c.BodyBytes = 8<<20 + 1 },
		func(c *Config) { c.RetainedBytes = 1 }, func(c *Config) { c.QueueSize = 257 },
		func(c *Config) { c.Rate = 0 }, func(c *Config) { c.Burst = 0 },
		func(c *Config) { c.LaneShare = -1 }, func(c *Config) { c.LaneShare = 65 },
		func(c *Config) { c.LaneShare, c.RetainedBytes = 4, int64(c.BodyBytes+1+MetadataBytes) },
	} {
		c := testConfig()
		mutate(&c)
		if _, err := New[Snapshot](c); err == nil {
			t.Fatal("invalid limits accepted")
		}
	}
	s := Snapshot{Metadata: Metadata{RawQuery: "private"}, Body: Body{Prefix: []byte("private")}}
	for _, formatted := range []string{fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%s", &Record[Snapshot]{Snapshot: s})} {
		if strings.Contains(formatted, "private") {
			t.Fatal("capture leaked into diagnostics")
		}
	}
}

// One lane never holds more than its share of rows or bytes, so a busy
// producer cannot starve the others.
func TestLaneShareKeepsOtherLanesAdmitted(t *testing.T) {
	c := testConfig()
	c.QueueSize, c.LaneShare = 8, 4
	r, _ := New[Snapshot](c)
	busy, quiet := r.NewLane(), r.NewLane()
	var held []*Reservation[Snapshot]
	for range 3 {
		lease := busy.Reserve(1)
		if lease == nil {
			break
		}
		held = append(held, lease)
	}
	if len(held) != 2 || r.Drops()[DropCapacity] != 1 {
		t.Fatal("busy lane exceeded its row share", len(held), r.Drops())
	}
	if quiet.Reserve(1) == nil {
		t.Fatal("quiet lane was starved by a busy lane")
	}
	share := c.RetainedBytes / 4
	if quiet.Reserve(share) != nil {
		t.Fatal("lane admitted beyond its byte share")
	}
	other := r.NewLane()
	if lease := other.Reserve(share); lease == nil || lease.Grow(1) {
		t.Fatal("lane byte share was not enforced on growth")
	}
	for _, lease := range held {
		lease.Complete(Snapshot{}, 1)
	}
	first := r.Drain()
	if first.DroppedBefore != 1 {
		t.Fatal("busy lane loss was not carried by its own record", first.DroppedBefore)
	}
}

func TestLaneRateIsPerLaneAndLossReturnsToItsLane(t *testing.T) {
	c := testConfig()
	c.Rate, c.Burst, c.LaneShare = 1, 1, 4
	r, _ := New[Snapshot](c)
	a, b := r.NewLane(), r.NewLane()
	now := time.Now()
	first := a.reserve(now, 1)
	if first == nil || a.reserve(now, 1) != nil {
		t.Fatal("lane rate was not applied")
	}
	second := b.reserve(now, 1)
	if second == nil {
		t.Fatal("one lane's rate consumed another lane's tokens")
	}
	first.Complete(Snapshot{}, 1)
	second.Complete(Snapshot{}, 1)
	ra, rb := r.Drain(), r.Drain()
	if ra.DroppedBefore != 1 || rb.DroppedBefore != 0 {
		t.Fatal("loss reached the wrong lane", ra.DroppedBefore, rb.DroppedBefore)
	}
	ra.Lose(DropWrite)
	ra.Lose(DropWrite)
	rb.Release()
	if r.Drops()[DropWrite] != 1 {
		t.Fatal("lost record counted more than once", r.Drops())
	}
	next := a.reserve(now.Add(time.Second), 1)
	next.Complete(Snapshot{}, 1)
	if record := r.Drain(); record.DroppedBefore != 2 {
		t.Fatal("lost record and its carried loss did not return to its lane", record.DroppedBefore)
	}
	if other := b.reserve(now.Add(time.Second), 1); other == nil {
		t.Fatal("lane b refused")
	} else {
		other.Complete(Snapshot{}, 1)
		if record := r.Drain(); record.DroppedBefore != 0 {
			t.Fatal("another lane's loss leaked into lane b")
		}
	}
}

func TestGrowRefusalKeepsTheRecordAndSettlesToRetained(t *testing.T) {
	c := testConfig()
	c.RetainedBytes = int64(c.BodyBytes + 1 + MetadataBytes)
	r, _ := New[Snapshot](c)
	lane := r.NewLane()
	lease := lane.Reserve(MetadataBytes)
	if lease == nil || !lease.Grow(int64(c.BodyBytes)) || lease.Grow(2) {
		t.Fatal("growth was not bounded by the process budget")
	}
	if r.Drops() != ([DropReasons]uint64{}) {
		t.Fatal("refused growth dropped a record", r.Drops())
	}
	lease.Complete(Snapshot{}, 1<<40)
	if _, retained := r.Usage(); retained != int64(MetadataBytes+c.BodyBytes) {
		t.Fatal("completion retained more than was admitted", retained)
	}
	if lease.Grow(1) {
		t.Fatal("a completed reservation grew")
	}
	r.Drain().Release()
	if slots, bytes := r.Usage(); slots != 0 || bytes != 0 {
		t.Fatal(slots, bytes)
	}
}

func TestHeaderBudgetIsDeterministic(t *testing.T) {
	header := stdhttp.Header{}
	for i := range 200 {
		header.Set(fmt.Sprintf("X-Field-%03d", i), strings.Repeat("v", 200))
	}
	want, truncated := Headers(header)
	if !truncated || len(want) == 0 || want[0].Name != "X-Field-000" {
		t.Fatal("header snapshot did not keep the lowest canonical names first", len(want), truncated)
	}
	for i := 1; i < len(want); i++ {
		if want[i-1].Name >= want[i].Name {
			t.Fatal("header snapshot is not sorted by canonical name")
		}
	}
	for range 20 {
		got, _ := Headers(header)
		if len(got) != len(want) || got[len(got)-1].Name != want[len(want)-1].Name {
			t.Fatal("header budget depends on map iteration order")
		}
	}
}
