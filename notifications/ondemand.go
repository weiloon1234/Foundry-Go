package notifications

import (
	"context"
	"encoding/json"
	"slices"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// OnDemandRecipient names the shared recipient of on-demand bindings.
const OnDemandRecipient RecipientName = "foundry.on-demand"

// MaxRouteAddressBytes bounds Route.Address.
const MaxRouteAddressBytes = 512

// onDemandToken is the single recipient declaration all on-demand bindings share.
var onDemandToken = &declarationToken{}

// Route is an on-demand notification recipient without an application model:
// an email address and/or an opaque address for custom channels (for example a
// phone number or webhook URL). It is stored with the notification as its
// identity, so treat it as private data. Routes have no inbox, preferences or
// eligibility check: the caller chose the destination explicitly.
type Route struct {
	Email   email.Address `json:"email,omitzero"`
	Address string        `json:"address,omitempty"`
}

func (r Route) Validate() error {
	if r.Email == (email.Address{}) && r.Address == "" {
		return fault.New(fault.Invalid, "notification route requires an email or address")
	}
	if r.Email != (email.Address{}) && r.Email.Validate() != nil {
		return fault.New(fault.Invalid, "invalid notification route email")
	}
	if len(r.Address) > MaxRouteAddressBytes || !utf8.ValidString(r.Address) {
		return fault.New(fault.Invalid, "invalid notification route address")
	}
	for _, c := range r.Address {
		if c < ' ' || c == 0x7f {
			return fault.New(fault.Invalid, "invalid notification route address")
		}
	}
	return nil
}

func routeReference(key string) model.Reference[Route, string] {
	return model.NewReference[Route]("foundry_notification_routes", key, codec.String[string]())
}

// Reference is the route's typed identity for Binding.Capture and Send.
func (r Route) Reference() (model.Reference[Route, string], error) {
	if err := r.Validate(); err != nil {
		return model.Reference[Route, string]{}, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return model.Reference[Route, string]{}, fault.New(fault.Invalid, "invalid notification route")
	}
	return routeReference(string(data)), nil
}
func (r Route) FoundryIdentity() (model.Identity, error) {
	reference, err := r.Reference()
	if err != nil {
		return model.Identity{}, err
	}
	return reference.Identity()
}

// routeFromIdentity restores and revalidates a stored route.
func routeFromIdentity(identity model.Identity) (Route, error) {
	reference, err := routeReference("").Parse(identity)
	if err != nil {
		return Route{}, err
	}
	var route Route
	if err := json.Unmarshal([]byte(reference.Key()), &route); err != nil {
		return Route{}, fault.New(fault.Invalid, "stored notification route is invalid")
	}
	return route, route.Validate()
}

// BindOnDemand binds definition to on-demand routes. Channels receive the
// Route as their model: use Email with a renderer addressing route.Email, or a
// Custom transport using route.Address. Database (inbox) channels are rejected.
// Capture with route.Reference(); storage, retries, delivery jobs and operator
// resolution work as for model recipients.
func BindOnDemand[P any](definition Definition[P], channels ...Channel[Route, P]) Binding[Route, string, P] {
	channels = slices.Clone(channels)
	r := &registration{recipientToken: onDemandToken, definitionToken: definition.token,
		key: registrationKey{OnDemandRecipient, definition.name, definition.version}, scope: digest(string(OnDemandRecipient))}
	for _, c := range channels {
		r.channels = append(r.channels, c.definition)
	}
	r.validate = func() error {
		if err := definition.Validate(); err != nil {
			return err
		}
		if len(channels) == 0 || len(channels) > MaxChannels {
			return invalid()
		}
		seen := make(map[ChannelID]bool)
		for _, c := range channels {
			if err := c.Validate(); err != nil {
				return err
			}
			if seen[c.definition.id] {
				return fault.New(fault.Duplicate, "notification channel is already bound")
			}
			seen[c.definition.id] = true
			if c.definition.kind == "database" || c.definition.recipient != nil {
				return fault.New(fault.Invalid, "on-demand notifications have no inbox or recipient-bound channel")
			}
		}
		return nil
	}
	r.check = func(_ context.Context, identity model.Identity, _ ChannelID) (any, State, error) {
		route, err := routeFromIdentity(identity)
		if err != nil {
			return nil, Rejected, nil
		}
		return route, "", nil
	}
	r.render = func(ctx context.Context, index int, subject any, delivery DeliveryContext, data json.RawMessage) ([]byte, error) {
		payload, err := definition.payload.Decode(ctx, data, payloadLimits())
		if err != nil {
			return nil, err
		}
		return channels[index].render(ctx, subject.(Route), delivery, payload)
	}
	parse := func(identity model.Identity) error { _, err := routeFromIdentity(identity); return err }
	return Binding[Route, string, P]{definition: definition, registration: Registration{r}, parse: parse}
}
