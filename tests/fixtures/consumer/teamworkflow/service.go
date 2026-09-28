package teamworkflow

import (
	"context"
	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"strconv"
	"sync/atomic"
)

type Resources = modelbinding.Models[Team, Project]
type PatchRequest = modelbinding.Input[ProjectPath, http.NoQuery, Patch, Resources]
type SubmissionRequest = modelbinding.Input[SubmissionPath, http.NoQuery, SubmissionInput, Resources]
type Hooks struct {
	Calls         *atomic.Int32
	InTransaction func(context.Context, *database.Tx) error
	AfterCommit   func(context.Context) error
	Delivered     func(publisher.Message)
}
type Service struct {
	db     *database.DB
	store  *idempotency.Store
	outbox *events.Outbox
	hooks  Hooks
}

func NewService(db *database.DB, store *idempotency.Store, bus *events.Bus, hooks Hooks) (*Service, error) {
	producer, err := events.PrepareOutbox("workflow", bus)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, store: store, outbox: producer, hooks: hooks}, nil
}
func present(project Project) ProjectView {
	return ProjectView{ID: project.ID, Slug: project.Slug, Title: project.Title, Budget: project.Budget}
}
func (s *Service) Patch(ctx context.Context, _ Actor, in PatchRequest) (ActionEnvelope, error) {
	draft := ProjectDraft{}
	if title, supplied := in.Request.Body.Title.Get(); supplied {
		if text, present := title.Get(); present {
			draft = draft.SetTitle(text)
		} else {
			draft = draft.ClearTitle()
		}
	}
	if budget, supplied := in.Request.Body.Budget.Get(); supplied {
		draft = draft.SetBudget(budget)
	}
	project := in.Model.Child
	if !draft.IsEmpty() {
		err := s.db.Transaction(ctx, func(tx *database.Tx) error {
			var err error
			project, err = QueryWorkflowProjects().Update(ctx, tx, project.ID, draft)
			return err
		})
		if err != nil {
			return ActionEnvelope{}, err
		}
	}
	action, err := ActionFromUpdated(present(project))
	return genericdto.Envelope[Action]{Data: action, Trace: "workflow"}, err
}
func (s *Service) Submit(ctx context.Context, tx *database.Tx, _ Actor, in SubmissionRequest) (ActionEnvelope, error) {
	if s.hooks.Calls != nil {
		s.hooks.Calls.Add(1)
	}
	row, err := QueryWorkflowSubmissions().Create(ctx, tx, SubmissionDraft{}.SetProjectID(in.Model.Child.ID).SetName(in.Request.Body.Name))
	if err != nil {
		return ActionEnvelope{}, err
	}
	receipt := SubmissionView{ID: row.ID, ProjectID: row.ProjectID, Name: row.Name}
	if _, err := Submitted.Enqueue(ctx, tx, s.outbox, receipt); err != nil {
		return ActionEnvelope{}, err
	}
	if s.hooks.InTransaction != nil {
		if err := s.hooks.InTransaction(ctx, tx); err != nil {
			return ActionEnvelope{}, err
		}
	}
	if s.hooks.AfterCommit != nil {
		if err := tx.AfterCommit(s.hooks.AfterCommit); err != nil {
			return ActionEnvelope{}, err
		}
	}
	if in.Request.Body.Reject {
		return ActionEnvelope{}, Rejected
	}
	action, err := ActionFromQueued(receipt)
	return genericdto.Envelope[Action]{Data: action, Trace: "workflow"}, err
}
func submissionScope(_ context.Context, actor Actor, _ SubmissionRequest) (idempotency.Scope, error) {
	return idempotency.NewScope(actor.Tenant, strconv.FormatInt(actor.ID, 10))
}
