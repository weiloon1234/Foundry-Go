package schedule

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Occurrence struct{}
type OccurrenceID = model.ID[Occurrence]
type Execution struct{}
type ExecutionID = model.ID[Execution]

// Invocation carries stable occurrence identity, not a live service container.
// The same namespace/group/schedule/UTC instant has the same ID after failover.
// It is an idempotency key, not proof of exactly-once business effects.
type Invocation struct {
	Schedule   ID
	Occurrence OccurrenceID
	IntendedAt time.Time
	Origin     attribution.Origin
}

func occurrence(namespace keyspace.Namespace, group Group, id ID, at time.Time) Invocation {
	hash := sha256.New()
	for _, value := range []string{"foundry.schedule.v1", namespace.Application, namespace.Environment, string(group), string(id)} {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	var stamp [12]byte
	binary.BigEndian.PutUint64(stamp[:8], uint64(at.Unix()))
	binary.BigEndian.PutUint32(stamp[8:], uint32(at.Nanosecond()))
	hash.Write(stamp[:])
	var bytes [16]byte
	copy(bytes[:], hash.Sum(nil))
	bytes[6] = (bytes[6] & 0x0f) | 0x80 // RFC 9562 application-defined UUIDv8.
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	origin, _ := (attribution.Origin{}).WithSystem(attribution.SystemID(id)) // ID already validated.
	return Invocation{Schedule: id, Occurrence: model.IDFromBytes[Occurrence](bytes), IntendedAt: at.UTC(), Origin: origin}
}

type invocationKey struct{}
type invocationFrame struct {
	scheduler  *Scheduler
	invocation Invocation
	active     atomic.Bool
}

// Current is valid only while this scheduler owns the active callback. A saved
// context ceases to represent live execution after all hooks/handlers return.
func Current(ctx context.Context) (Invocation, bool) {
	if ctx == nil {
		return Invocation{}, false
	}
	frame, _ := ctx.Value(invocationKey{}).(*invocationFrame)
	if frame == nil || !frame.active.Load() {
		return Invocation{}, false
	}
	return frame.invocation, true
}
