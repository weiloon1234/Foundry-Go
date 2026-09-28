package background

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	jobcommand "github.com/weiloon1234/Foundry-Go/jobs/command"
)

// RetryWelcome retains the payload-specific ID. The caller obtains and saves
// token from the failed record before making an operator-approved retry.
func RetryWelcome(ctx context.Context, dispatcher *jobs.Dispatcher, id jobs.ID[Welcome], token jobs.RetryToken) (bool, error) {
	return WelcomeJob.Retry(ctx, dispatcher, id, "communications", token)
}

func ConfiguredWelcome(sender WelcomeSender, middleware jobs.Middleware[Welcome]) application.JobDeclaration {
	return application.JobWith(WelcomeJob, func(services application.Services) (jobs.Handler[Welcome], jobs.HandlerOptions[Welcome], error) {
		return func(ctx context.Context, input Welcome) error { return sender.SendWelcome(ctx, input.UserID) },
			jobs.HandlerOptions[Welcome]{Middleware: []jobs.Middleware[Welcome]{middleware}}, nil
	})
}

// JobCommands belongs in the application's CLI registry, alongside migrations
// and domain commands. The globally installed development CLI cannot boot it.
func JobCommands() (cli.Declaration, error) {
	return jobcommand.Declaration(func(resolver foundation.Resolver) (*jobs.Connections, error) {
		services, err := application.FromResolver(resolver)
		if err != nil {
			return nil, err
		}
		return services.Jobs, nil
	})
}
