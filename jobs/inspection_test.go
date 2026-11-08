package jobs_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func TestQuarantineInspectionOptionalBackendAndValidation(t *testing.T) {
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	f.enqueue(t)
	page, err := f.dispatcher.Quarantined(t.Context(), f.key.Queue(), jobs.QuarantineOptions{Limit: 20})
	if err != nil || len(page.Records) != 0 || !page.Next.IsZero() {
		t.Fatal(page, err)
	}
	for _, limit := range []int{-1, 0, jobs.MaxListLimit + 1} {
		if _, err := f.dispatcher.Quarantined(t.Context(), f.key.Queue(), jobs.QuarantineOptions{Limit: limit}); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	registry, err := jobs.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := jobs.NewDispatcher(struct{ jobs.Backend }{f.backend}, registry, jobs.DefaultDispatchConfig(f.key.Namespace()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Quarantined(t.Context(), f.key.Queue(), jobs.QuarantineOptions{Limit: 20}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported quarantine inspection succeeded", err)
	}
}
