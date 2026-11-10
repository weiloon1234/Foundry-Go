package httpcapture

import (
	"fmt"
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

func TestReservationBudgetCoversActiveQueuedAndDrained(t *testing.T) {
	c := testConfig()
	c.RetainedBytes = int64(c.BodyBytes + 1 + MetadataBytes)
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	lease := r.Reserve()
	if lease == nil || r.Reserve() != nil {
		t.Fatal("active preread was not charged")
	}
	lease.Complete(Snapshot{Body: Body{Prefix: make([]byte, c.BodyBytes+1)}})
	lease.Complete(Snapshot{})
	if r.Reserve() != nil {
		t.Fatal("queue was not charged")
	}
	record := r.Drain()
	if record == nil || r.Reserve() != nil {
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
	if r.Reserve() != nil || r.Drops()[DropBytes] != 3 || r.Drops()[DropSealed] != 1 {
		t.Fatal(r.Drops())
	}
}

func TestRecorderRateCapacityDropsAndShrink(t *testing.T) {
	c := testConfig()
	c.Rate, c.Burst, c.QueueSize = 1, 1, 2
	r, _ := New(c)
	now := time.Now()
	first := r.reserve(now)
	if first == nil || r.reserve(now) != nil {
		t.Fatal("rate admission")
	}
	first.Complete(Snapshot{})
	if _, retained := r.Usage(); retained != MetadataBytes {
		t.Fatal(retained)
	}
	second := r.reserve(now.Add(time.Second))
	if second == nil || r.reserve(now.Add(2*time.Second)) != nil {
		t.Fatal("capacity admission")
	}
	second.Complete(Snapshot{})
	a, b := r.Drain(), r.Drain()
	if a.Snapshot.DroppedBefore != 1 || b.Snapshot.DroppedBefore != 1 || r.Drain() != nil {
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
	r, _ := New(testConfig())
	l := r.Reserve()
	r.Seal()
	if r.Reserve() != nil {
		t.Fatal("sealed admission")
	}
	l.Complete(Snapshot{})
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
	r, _ := New(testConfig())
	var wg sync.WaitGroup
	for range 128 {
		wg.Go(func() {
			if lease := r.Reserve(); lease != nil {
				lease.Complete(Snapshot{Body: Body{Prefix: []byte("private")}})
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
	} {
		c := testConfig()
		mutate(&c)
		if _, err := New(c); err == nil {
			t.Fatal("invalid limits accepted")
		}
	}
	s := Snapshot{Metadata: Metadata{RawQuery: "private"}, Body: Body{Prefix: []byte("private")}}
	for _, formatted := range []string{fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%s", &Record{Snapshot: s})} {
		if strings.Contains(formatted, "private") {
			t.Fatal("capture leaked into diagnostics")
		}
	}
}
