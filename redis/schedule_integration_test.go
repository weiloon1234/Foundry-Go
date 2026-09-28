package redis

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/scheduletest"
)

func TestRedisSchedulerLeadershipAndOwnedFailover(t *testing.T) {
	client, namespace, track := integrationAddresses(t, nil)
	scheduletest.Run(t, client, namespace, track)
}
