package bootstrap

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin profile-handler-contract
// ProfileInput combines the route, query and body types of the profile endpoint.
type ProfileInput = http.Input[http.NoPath, http.NoQuery, http.NoBody]
type profileService interface {
	Profile(context.Context, Member, ProfileInput) (Profile, error)
}

// doc-example:end profile-handler-contract

func guardedProfile(ctx context.Context, cookieAuthentication *http.Authentication, memberGuard auth.Guard[Member], profileEndpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, Profile], service profileService) (http.RouteRegistration, error) {
	// doc-example:begin application-bootstrap-3
	members, err := http.BindGuard(cookieAuthentication, memberGuard)
	if err != nil {
		return http.RouteRegistration{}, err
	}
	route := http.Authenticated(profileEndpoint, members).Handle(
		func(ctx context.Context, actor Member, input ProfileInput) (Profile, error) {
			return service.Profile(ctx, actor, input)
		},
	)
	// doc-example:end application-bootstrap-3
	return route, nil
}
