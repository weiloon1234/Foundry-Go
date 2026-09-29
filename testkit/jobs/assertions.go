package jobs

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

// maxPushedPages bounds one scan (100 records per page).
const maxPushedPages = 1000

// Pushed decodes the payloads of definition's jobs retained in queue, in every
// state, using the same strict JSON facility as workers. An empty queue selects
// the definition's policy queue. It pages through the bounded list API.
func Pushed[P any](t testing.TB, dispatcher *jobs.Dispatcher, definition jobs.Definition[P], queue jobs.Queue) []P {
	t.Helper()
	if queue == "" {
		queue = definition.Policy().Queue
	}
	options := jobs.ListOptions{Name: definition.Name(), Version: definition.Version(), Limit: jobs.MaxListLimit}
	var payloads []P
	for range maxPushedPages {
		page, err := dispatcher.List(t.Context(), queue, options)
		if err != nil {
			t.Fatalf("list test jobs: %v", err)
		}
		for _, record := range page.Records {
			if !options.Matches(record) {
				continue
			}
			// Payload also decrypts envelopes of an encrypted definition.
			payload, err := definition.Payload(t.Context(), record.Envelope)
			if err != nil {
				t.Fatal("decode test job payload failed")
			}
			payloads = append(payloads, payload)
		}
		if page.Next.IsZero() {
			return payloads
		}
		options.After = page.Next
	}
	t.Fatal("test job scan exceeded its bound")
	return nil
}

func countPushed[P any](t testing.TB, dispatcher *jobs.Dispatcher, definition jobs.Definition[P], queue jobs.Queue, match func(P) bool) int {
	t.Helper()
	count := 0
	for _, payload := range Pushed(t, dispatcher, definition, queue) {
		if match == nil || match(payload) {
			count++
		}
	}
	return count
}

// AssertPushed requires at least one retained job of definition whose payload
// satisfies match (nil matches any). Failure text never includes payloads.
func AssertPushed[P any](t testing.TB, dispatcher *jobs.Dispatcher, definition jobs.Definition[P], queue jobs.Queue, match func(P) bool) {
	t.Helper()
	if countPushed(t, dispatcher, definition, queue, match) == 0 {
		t.Errorf("no matching %s v%d job was pushed", definition.Name(), definition.Version())
	}
}

// AssertNotPushed requires that no retained job of definition satisfies match.
func AssertNotPushed[P any](t testing.TB, dispatcher *jobs.Dispatcher, definition jobs.Definition[P], queue jobs.Queue, match func(P) bool) {
	t.Helper()
	if got := countPushed(t, dispatcher, definition, queue, match); got != 0 {
		t.Errorf("%d matching %s v%d job(s) were pushed", got, definition.Name(), definition.Version())
	}
}

// AssertPushedCount requires exactly want retained jobs of definition that
// satisfy match (nil matches any).
func AssertPushedCount[P any](t testing.TB, dispatcher *jobs.Dispatcher, definition jobs.Definition[P], queue jobs.Queue, match func(P) bool, want int) {
	t.Helper()
	if got := countPushed(t, dispatcher, definition, queue, match); got != want {
		t.Errorf("pushed %s v%d job count: got %d, want %d", definition.Name(), definition.Version(), got, want)
	}
}
