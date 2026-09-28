package notifications

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Definition is the shared versioned input contract. Bind it to one or more
// typed recipient declarations; use generated DTO contracts as the schema SSOT.
type Definition[P any] struct {
	token   *declarationToken
	name    Name
	version Version
	payload contract.JSON[P]
}

func Define[P any](name Name, version Version, payload contract.JSON[P]) Definition[P] {
	return Definition[P]{token: &declarationToken{}, name: name, version: version, payload: payload}
}
func (d Definition[P]) Name() Name       { return d.name }
func (d Definition[P]) Version() Version { return d.version }
func (d Definition[P]) Validate() error {
	if d.token == nil || !semantic(string(d.name)) || d.version == 0 {
		return invalid()
	}
	return d.payload.Validate()
}

type Binding[M model.Identifiable, K, P any] struct {
	definition   Definition[P]
	recipient    Recipient[M, K]
	registration Registration
}
type registrationKey struct {
	recipient RecipientName
	name      Name
	version   Version
}
type registration struct {
	recipientToken  *declarationToken
	definitionToken *declarationToken
	key             registrationKey
	scope           string
	channels        []channelDefinition
	validate        func() error
	check           func(context.Context, model.Identity, ChannelID) (any, State, error)
	render          func(context.Context, int, any, DeliveryContext, json.RawMessage) ([]byte, error)
}
type Registration struct{ definition *registration }

// Bind is the typed registry boundary. A channel renderer for another recipient
// model or notification payload cannot be passed here.
func Bind[M model.Identifiable, K, P any](definition Definition[P], recipient Recipient[M, K], channels ...Channel[M, P]) Binding[M, K, P] {
	channels = slices.Clone(channels)
	r := &registration{recipientToken: recipient.token, definitionToken: definition.token,
		key: registrationKey{recipient.name, definition.name, definition.version}, scope: recipient.scope()}
	for _, c := range channels {
		r.channels = append(r.channels, c.definition)
	}
	r.validate = func() error {
		if err := definition.Validate(); err != nil {
			return err
		}
		if err := recipient.Validate(); err != nil {
			return err
		}
		if len(channels) == 0 || len(channels) > MaxChannels {
			return invalid()
		}
		seen := make(map[ChannelID]bool)
		database := false
		for _, c := range channels {
			if err := c.Validate(); err != nil {
				return err
			}
			if seen[c.definition.id] {
				return fault.New(fault.Duplicate, "notification channel is already bound")
			}
			seen[c.definition.id] = true
			if c.definition.recipient != nil && c.definition.recipient != recipient.token {
				return invalid()
			}
			if c.definition.kind == "database" {
				if database {
					return invalid()
				}
				database = true
			}
		}
		return nil
	}
	r.check = func(ctx context.Context, identity model.Identity, channel ChannelID) (any, State, error) {
		reference, err := recipient.provider.Parse(identity)
		if err != nil {
			return nil, Rejected, nil
		}
		var subject M
		var state State
		failure := callback.Isolated("check notification recipient", func() error {
			var err error
			subject, err = recipient.provider.Resolve(ctx, reference)
			if errorgraph.Is(err, auth.Unauthenticated) {
				state = Ineligible
				return nil
			}
			if err != nil {
				return err
			}
			allowed, err := recipient.preferences(ctx, subject, definition.name, channel)
			if err == nil && !allowed {
				state = Skipped
			}
			return err
		})
		if failure != nil {
			return nil, "", fault.New(fault.Internal, "notification recipient check failed")
		}
		return subject, state, nil
	}
	r.render = func(ctx context.Context, index int, subject any, delivery DeliveryContext, data json.RawMessage) ([]byte, error) {
		payload, err := definition.payload.Decode(ctx, data, payloadLimits())
		if err != nil {
			return nil, err
		}
		return channels[index].render(ctx, subject.(M), delivery, payload)
	}
	return Binding[M, K, P]{definition, recipient, Registration{r}}
}
func (b Binding[M, K, P]) Registration() Registration { return b.registration }
func (b Binding[M, K, P]) Validate() error {
	if b.registration.definition == nil {
		return invalid()
	}
	return b.registration.definition.validate()
}

// Registry is immutable and owns no resources. Duplicate recipient or payload
// names require reusing the original declaration, including across versions.
type Registry struct {
	entries map[registrationKey]*registration
}

func NewRegistry(items ...Registration) (*Registry, error) {
	r := &Registry{entries: make(map[registrationKey]*registration)}
	recipients := make(map[RecipientName]*declarationToken)
	definitions := make(map[[2]string]*declarationToken)
	for _, item := range items {
		d := item.definition
		if d == nil || d.validate == nil {
			return nil, invalid()
		}
		if err := d.validate(); err != nil {
			return nil, err
		}
		if _, exists := r.entries[d.key]; exists {
			return nil, fault.New(fault.Duplicate, "notification binding is already registered")
		}
		if previous := recipients[d.key.recipient]; previous != nil && previous != d.recipientToken {
			return nil, fault.New(fault.Duplicate, "notification recipient name is already declared")
		}
		recipients[d.key.recipient] = d.recipientToken
		key := [2]string{string(d.key.name), stringVersion(d.key.version)}
		if previous := definitions[key]; previous != nil && previous != d.definitionToken {
			return nil, fault.New(fault.Duplicate, "notification name and version are already declared")
		}
		definitions[key] = d.definitionToken
		r.entries[d.key] = d
	}
	return r, nil
}
