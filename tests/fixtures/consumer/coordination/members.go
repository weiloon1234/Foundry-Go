// Package coordination verifies model-owned lease keys from an independent consumer.
package coordination

import (
	"context"
	"time"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberRefresh is one reusable declaration; distinct model IDs cannot be mixed.
var MemberRefresh = lease.Define("member-refresh", keyspace.TextKeys[model.ID[mutatorqueries.Member]]())

type MemberLeases = lease.Leases[model.ID[mutatorqueries.Member]]

func Bind(manager *lease.Manager) (MemberLeases, error) { return MemberRefresh.Bind(manager) }

// RefreshMember supplies domain work. Foundry owns waiting, heartbeat and cleanup.
func RefreshMember(ctx context.Context, locks MemberLeases, id model.ID[mutatorqueries.Member], refresh func(context.Context, model.ID[mutatorqueries.Member]) error) (bool, error) {
	return locks.With(ctx, id, 30*time.Second, 2*time.Second, func(ctx context.Context) error { return refresh(ctx, id) })
}

// TryRefresh demonstrates explicit guard ownership for a scope managed by its caller.
func TryRefresh(ctx context.Context, locks MemberLeases, id model.ID[mutatorqueries.Member]) (*lease.Guard, bool, error) {
	return locks.TryAcquire(ctx, id, 30*time.Second)
}
