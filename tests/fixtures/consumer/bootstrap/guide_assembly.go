package bootstrap

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin application-bootstrap-3
// ProfileInput combines the route, query and body types of the profile endpoint.
type ProfileInput = http.Input[http.NoPath, http.NoQuery, http.NoBody]
type profileService interface {
	Profile(context.Context, Member, ProfileInput) (Profile, error)
}

func guardedProfile(ctx context.Context, cookieAuthentication *http.Authentication, memberGuard auth.Guard[Member], profileEndpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, Profile], service profileService) error {
	members, err := http.BindGuard(cookieAuthentication, memberGuard)
	if err != nil {
		return err
	}
	route := http.Authenticated(profileEndpoint, members).Handle(
		func(ctx context.Context, actor Member, input ProfileInput) (Profile, error) {
			return service.Profile(ctx, actor, input)
		},
	)
	_ = route
	return nil
}

// doc-example:end application-bootstrap-3
