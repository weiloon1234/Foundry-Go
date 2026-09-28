package schedule_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/scheduletest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

func TestSharedSchedulerCoordination(t *testing.T) {
	backend, err := memory.New(128)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	scheduletest.Run(t, backend, keyspace.Namespace{Application: "schedule", Environment: "test"}, nil)
}
