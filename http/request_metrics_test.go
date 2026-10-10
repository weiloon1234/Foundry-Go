package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/observability"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRouteMetricsAggregateBoundedIntervals(t *testing.T) {
	m, err := NewRequestMetrics(2)
	if err != nil {
		t.Fatal(err)
	}
	event := RequestEvent{Route: "accounts.show", Method: GET, Result: observability.Result{Status: 200}, Duration: 20 * time.Millisecond}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				m.ObserveRequest(context.Background(), event)
			}
		})
	}
	wg.Wait()
	m.ObserveRequest(context.Background(), RequestEvent{Method: "ATTACKER-METHOD", Result: observability.Result{Status: 404}, Duration: 2 * time.Second})
	m.ObserveRequest(context.Background(), RequestEvent{Route: "another", Method: GET, Result: observability.Result{Status: 200}})
	snapshot := m.Snapshot()
	if len(snapshot.Series) != 2 || snapshot.Dropped != 1 {
		t.Fatal(snapshot)
	}
	if snapshot.Series[1].Count != 400 || snapshot.Series[1].Buckets[2] != 400 || snapshot.Series[1].Buckets[0] != 0 {
		t.Fatal(snapshot.Series[1])
	}
	snapshot.Series[1].Buckets[2] = 999
	if m.Snapshot().Series[1].Buckets[2] != 400 {
		t.Fatal("snapshot aliases collector")
	}
	var output strings.Builder
	if err := m.WritePrometheus(&output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "ATTACKER-METHOD") || !strings.Contains(output.String(), "le=\"+Inf\"") {
		t.Fatal(output.String())
	}
	if len(m.Drain().Series) != 2 || len(m.Drain().Series) != 0 {
		t.Fatal("interval drain lost ownership")
	}
}
