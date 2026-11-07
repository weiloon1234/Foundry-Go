package background

import (
	"context"
	"time"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

// doc-example:begin configured-job
func ConfigureWelcome(builder *application.Builder, connection jobs.ConnectionName, sender WelcomeSender) *application.Builder {
	return builder.Jobs(application.Job(WelcomeJob, func(application.Services) (jobs.Handler[Welcome], error) {
		return func(ctx context.Context, payload Welcome) error {
			return sender.SendWelcome(ctx, payload.UserID)
		}, nil
	}).On(connection))
}

// doc-example:end configured-job

// doc-example:begin enqueue-job
type WelcomeProducer struct{ outbox *jobs.Outbox }

func NewWelcomeProducer(services application.Services, connection jobs.ConnectionName) (*WelcomeProducer, error) {
	producer, err := services.JobOutbox(connection)
	if err != nil {
		return nil, err
	}
	return &WelcomeProducer{outbox: producer}, nil
}

func (p *WelcomeProducer) Enqueue(ctx context.Context, tx *database.Tx, id model.ID[models.User]) (outbox.ID[Welcome], error) {
	return WelcomeJob.Enqueue(ctx, tx, p.outbox, Welcome{UserID: id}, jobs.Options[Welcome]{})
}

// doc-example:end enqueue-job

// doc-example:begin calendar-job
func WelcomeSchedule(services application.Services, dispatcher *jobs.Dispatcher, recipient model.ID[models.User]) (schedule.Declaration, error) {
	handler, err := schedule.JobTarget(dispatcher, WelcomeJob, func(context.Context, schedule.Invocation) (Welcome, error) {
		return Welcome{UserID: recipient}, nil
	})
	if err != nil {
		return schedule.Declaration{}, err
	}
	declaration, err := services.Calendar().DailyAt("members.daily-welcome", "09:00", handler)
	if err != nil {
		return schedule.Declaration{}, err
	}
	options := declaration.Options()
	options.WithoutOverlap = true
	options.Timeout = time.Minute
	return declaration.With(options)
}

// doc-example:end calendar-job
