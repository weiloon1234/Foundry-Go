package bootstrap

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// ProfileJob is a domain descriptor shared by registration and dispatch.
var ProfileJob = jobs.Define[Profile]("bootstrap.profile", 1, jobs.DefaultPolicy("profiles"))

type ProfileDelivery struct {
	work   jobs.Bound[Profile]
	mailer *email.Mailer
}

func NewProfileDelivery(services application.Services) (ProfileDelivery, error) {
	connection, err := services.JobConnection()
	if err != nil {
		return ProfileDelivery{}, err
	}
	bound, err := ProfileJob.On(connection)
	if err != nil {
		return ProfileDelivery{}, err
	}
	mailer, err := services.Mailers.Mailer("default")
	if err != nil {
		return ProfileDelivery{}, err
	}
	return ProfileDelivery{bound, mailer}, nil
}
func (d ProfileDelivery) Queue(ctx context.Context, profile Profile) (jobs.Receipt[Profile], error) {
	return d.work.Dispatch(ctx, profile, jobs.Options[Profile]{})
}
