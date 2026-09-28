package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Anonymous struct{}
type channelToken struct{ marker byte }

// SubjectReference is immutable authenticated attribution for cleanup and
// management. It is not a credential and cannot authenticate another operation.
type SubjectReference struct {
	Guard    auth.GuardName
	Provider auth.ProviderName
	Identity model.Identity
}

type MessageContext[R, S any] struct {
	Publisher  *Hub
	Connection ConnectionID
	Target     Target[R]
	Subject    S
}

// LeaveContext deliberately contains no retained model or authentication scope.
type LeaveContext[R any] struct {
	Publisher  *Hub
	Connection ConnectionID
	Target     Target[R]
	Subject    SubjectReference
}
type Hooks[R, S any] struct {
	Joined func(context.Context, MessageContext[R, S]) error
	Left   func(context.Context, LeaveContext[R]) error
}

// Channel binds a nominal owner, room key and concrete authenticated subject.
// Use a distinct owner type per channel. Runtime declaration identity also rejects
// separately declared channels sharing the same owner or string name.
type Channel[C, R, S any] struct {
	_         [0]*C
	token     *channelToken
	id        ChannelID
	rooms     Rooms[R]
	guard     auth.Guard[S]
	private   bool
	owned     bool
	authorize func(context.Context, S, Target[R]) error
	resolve   func(context.Context) (S, SubjectReference, attribution.Origin, error)
	hooks     Hooks[R, S]
	presence  *presenceDefinition[S]
	replay    ReplayConfig
	err       error
}

func Public[C, R any](id ChannelID, rooms Rooms[R]) Channel[C, R, Anonymous] {
	return Channel[C, R, Anonymous]{token: &channelToken{}, id: id, rooms: rooms,
		resolve: func(ctx context.Context) (Anonymous, SubjectReference, attribution.Origin, error) {
			return Anonymous{}, SubjectReference{}, attribution.FromContext(ctx), nil
		}}
}

// Private always requires explicit room/channel authorization after the fresh
// typed guard check. Matching a guard alone does not authorize arbitrary rooms.
func Private[C, R any, S model.Identifiable](id ChannelID, rooms Rooms[R], guard auth.Guard[S], authorize func(context.Context, S, Target[R]) error) Channel[C, R, S] {
	c := Channel[C, R, S]{token: &channelToken{}, id: id, rooms: rooms, guard: guard, private: true, authorize: authorize}
	c.resolve = func(ctx context.Context) (S, SubjectReference, attribution.Origin, error) {
		subject, err := guard.Require(ctx)
		if err != nil {
			return *new(S), SubjectReference{}, attribution.Origin{}, err
		}
		origin, err := guard.Origin(ctx)
		if err != nil {
			return *new(S), SubjectReference{}, attribution.Origin{}, err
		}
		identity, present := origin.Model()
		if !present {
			return *new(S), SubjectReference{}, attribution.Origin{}, fault.New(fault.Internal, "verified guard has no identity")
		}
		return subject, SubjectReference{Guard: guard.Name(), Provider: guard.ProviderName(), Identity: identity}, origin, nil
	}
	return c
}

// OwnedRooms permits only a concrete room equal to the authenticated model's
// stored key. Reuse the generated zero model's reference metadata; presentation
// getters never decide ownership. Channel-wide subscription is forbidden.
func OwnedRooms[C any, R comparable, S model.Identifiable](id ChannelID, rooms Rooms[R], guard auth.Guard[S], reference model.Reference[S, R]) Channel[C, R, S] {
	c := Private[C](id, rooms, guard, func(ctx context.Context, subject S, target Target[R]) error {
		room, present := target.Room.Get()
		if !present {
			return auth.Forbidden
		}
		origin, err := guard.Origin(ctx)
		if err != nil {
			return err
		}
		identity, present := origin.Model()
		if !present {
			return auth.Unauthenticated
		}
		actual, err := reference.Parse(identity)
		if err != nil {
			return err
		}
		if actual.Key() != room {
			return auth.Forbidden
		}
		return ctx.Err()
	})
	c.owned = true
	c.err = reference.Validate()
	return c
}

func (c Channel[C, R, S]) ID() ChannelID                                { return c.id }
func (c Channel[C, R, S]) WithHooks(hooks Hooks[R, S]) Channel[C, R, S] { c.hooks = hooks; return c }
func (c Channel[C, R, S]) WithReplay(config ReplayConfig) Channel[C, R, S] {
	c.replay = config
	return c
}
func (c Channel[C, R, S]) Validate() error {
	if c.err != nil {
		return c.err
	}
	if c.token == nil || !identifier.Semantic(string(c.id)) || c.resolve == nil {
		return fault.New(fault.Invalid, "invalid WebSocket channel")
	}
	if err := c.rooms.Validate(); err != nil {
		return err
	}
	if err := c.replay.Validate(); err != nil {
		return err
	}
	if c.private {
		if c.authorize == nil {
			return fault.New(fault.Invalid, "private channel requires authorization")
		}
		if err := c.guard.Validate(); err != nil {
			return err
		}
	}
	if c.presence != nil {
		return c.presence.validate()
	}
	return nil
}

type accessResult struct {
	context   context.Context
	subject   any
	target    any
	reference SubjectReference
	origin    attribution.Origin
}

func (c Channel[C, R, S]) check(ctx context.Context, room *string) (accessResult, error) {
	target, err := c.rooms.decode(ctx, room)
	if err != nil {
		if errorgraph.Is(err, fault.Panicked) {
			return accessResult{}, err
		}
		return accessResult{}, Malformed
	}
	subject, reference, origin, err := c.resolve(ctx)
	if err != nil {
		return accessResult{}, err
	}
	if c.private {
		ctx, err = attribution.WithContext(ctx, origin)
		if err != nil {
			return accessResult{}, err
		}
	}
	if c.authorize != nil {
		if err := c.authorize(ctx, subject, target); err != nil {
			return accessResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return accessResult{}, err
	}
	return accessResult{context: ctx, subject: subject, target: target, reference: reference, origin: origin}, nil
}
