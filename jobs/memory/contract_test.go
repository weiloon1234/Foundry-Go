package memory_test

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/jobtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func TestSharedJobContract(t *testing.T) {
	jobtest.Run(t, func(t *testing.T) jobtest.Fixture {
		f := setup(t, 5)
		return jobtest.Fixture{Backend: f.backend, Key: f.key, Advance: f.clock.Advance, Expire: func(jobs.Reservation) { f.clock.Advance(time.Minute) }}
	})
}
