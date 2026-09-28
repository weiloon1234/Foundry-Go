// Package background demonstrates framework-owned jobs with domain-only payloads
// and services. It is a consumer contract fixture, not a starter application.
package background

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Welcome struct {
	UserID model.ID[models.User] `json:"user_id"`
}
type RemoveExport struct {
	Name string `json:"name"`
}

var WelcomeJob = jobs.Define[Welcome]("members.welcome", 1, jobs.DefaultPolicy("communications"))

type WelcomeSender interface {
	SendWelcome(context.Context, model.ID[models.User]) error
}

func WelcomeDeclaration(sender WelcomeSender) (jobs.Declaration, error) {
	return WelcomeJob.Declare(func(ctx context.Context, payload Welcome) error { return sender.SendWelcome(ctx, payload.UserID) })
}
func DispatchWelcome(ctx context.Context, dispatcher *jobs.Dispatcher, id model.ID[models.User]) (jobs.Receipt[Welcome], error) {
	return WelcomeJob.Dispatch(ctx, dispatcher, Welcome{UserID: id}, jobs.Options[Welcome]{})
}
