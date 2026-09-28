package websocket

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type presenceToken struct{ marker byte }

type presenceDefinition[S any] struct {
	verify      func(context.Context, []byte, contract.JSONLimits) error
	token       *presenceToken
	validate    func() error
	description func() (contract.Schema, error)
	encode      func(context.Context, S, contract.JSONLimits) ([]byte, error)
}
type PresenceChannel[C, R, S, D any] struct {
	channel Channel[C, R, S]
	payload contract.JSON[D]
}

// WithPresence requires an explicit safe DTO descriptor and authenticated model.
// contract.JSON rejects persistence models. Only the DTO bytes and immutable
// subject reference survive the operation; full models are never stored.
func WithPresence[C, R any, S model.Identifiable, D any](channel Channel[C, R, S], payload contract.JSON[D], member func(context.Context, S) (D, error)) PresenceChannel[C, R, S, D] {
	channel.presence = &presenceDefinition[S]{verify: func(ctx context.Context, data []byte, limits contract.JSONLimits) error {
		_, err := payload.Decode(ctx, data, limits)
		return err
	}, token: &presenceToken{}, validate: func() error {
		if !channel.private || member == nil {
			return fault.New(fault.Invalid, "presence requires a private channel and safe member mapper")
		}
		return payload.Validate()
	}, description: payload.Description, encode: func(ctx context.Context, subject S, limits contract.JSONLimits) ([]byte, error) {
		data, err := member(ctx, subject)
		if err != nil {
			return nil, err
		}
		return payload.Encode(ctx, data, limits)
	}}
	return PresenceChannel[C, R, S, D]{channel: channel, payload: payload}
}
func (p PresenceChannel[C, R, S, D]) Channel() Channel[C, R, S] { return p.channel }

type Member[D any] struct {
	ID          MemberID
	Data        D
	Connections int
}

// Members is a trusted server-side inspection API, not an authorization check.
// Local results describe this Hub only; distributed results use its authority.
func (p PresenceChannel[C, R, S, D]) Members(ctx context.Context, hub *Hub, target Target[R]) ([]Member[D], error) {
	if err := hub.validateChannel(p.channel.token, p.channel.id); err != nil {
		return nil, err
	}
	if p.channel.presence == nil || hub.registry.channels[p.channel.id].presence != p.channel.presence.token {
		return nil, fault.New(fault.Invalid, "presence declaration is not registered")
	}
	ctx, finish, err := hub.operation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	key := subscriptionKey{channel: p.channel.id}
	if room, ok := target.Room.Get(); ok {
		text, err := p.channel.rooms.encode(ctx, room)
		if err != nil {
			return nil, err
		}
		key.room = text
		key.hasRoom = true
	}
	var frames []MemberFrame
	if hub.cluster != nil {
		snapshot, err := hub.clusterMembers(ctx, key)
		if err != nil {
			return nil, err
		}
		frames = snapshot.Members
	} else {
		hub.mu.Lock()
		frames = hub.memberFrames(key)
		hub.mu.Unlock()
	}
	result := make([]Member[D], 0, len(frames))
	for _, frame := range frames {
		data, err := p.payload.Decode(ctx, frame.Data, hub.config.Payload)
		if err != nil {
			return nil, err
		}
		result = append(result, Member[D]{ID: frame.ID, Data: data, Connections: frame.Connections})
	}
	return result, ctx.Err()
}
func memberID(subject SubjectReference) (MemberID, error) {
	if subject.Identity.IsZero() {
		return "", fault.New(fault.Invalid, "presence requires authenticated identity")
	}
	identity, err := json.Marshal(subject.Identity)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte("foundry.websocket.member.v1\x00"))
	h.Write([]byte(subject.Guard))
	h.Write([]byte{0})
	h.Write([]byte(subject.Provider))
	h.Write([]byte{0})
	h.Write(identity)
	return MemberID(hex.EncodeToString(h.Sum(nil))), nil
}

type presenceMember struct {
	data        []byte
	connections map[ConnectionID]bool
}

// All presence helpers below run under Hub.mu and copy data at public boundaries.
func (h *Hub) memberFrames(key subscriptionKey) []MemberFrame {
	group := h.presence[key]
	result := make([]MemberFrame, 0, len(group))
	for id, member := range group {
		result = append(result, MemberFrame{ID: id, Data: slices.Clone(member.data), Connections: len(member.connections)})
	}
	slices.SortFunc(result, func(a, b MemberFrame) int { return cmp.Compare(a.ID, b.ID) })
	return result
}

// Count uses the same authoritative safe-member snapshot as Members. It counts
// subjects, not tabs, and reports errors instead of a fallback local cluster count.
func (p PresenceChannel[C, R, S, D]) Count(ctx context.Context, hub *Hub, target Target[R]) (int, error) {
	members, err := p.Members(ctx, hub, target)
	if err != nil {
		return 0, err
	}
	return len(members), nil
}
