package bootstrap

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin profile-handler-contract
// ProfileInput combines the route, query and body types of the profile endpoint.
type ProfileInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]
type profileService interface {
	Profile(context.Context, Member, ProfileInput) (Profile, error)
}

// doc-example:end profile-handler-contract

func guardedProfile(ctx context.Context, cookieAuthentication *foundryhttp.Authentication, memberGuard auth.Guard[Member], profileEndpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Profile], service profileService) (foundryhttp.RouteRegistration, error) {
	// doc-example:begin application-bootstrap-3
	members, err := foundryhttp.BindGuard(cookieAuthentication, memberGuard)
	if err != nil {
		return foundryhttp.RouteRegistration{}, err
	}
	route := foundryhttp.Authenticated(profileEndpoint, members, members.AnyActor()).Handle(
		func(ctx context.Context, actor Member, input ProfileInput) (Profile, error) {
			return service.Profile(ctx, actor, input)
		},
	)
	// doc-example:end application-bootstrap-3
	return route, nil
}
