package teamworkflow

import (
	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:path pattern=/teams/{team}/projects/{project}
type ProjectPath struct {
	Team    int64
	Project Slug
}

//foundry:path pattern=/teams/{team}/projects/{project}/submissions
type SubmissionPath struct {
	Team    int64
	Project Slug
}

//foundry:dto
type Patch struct {
	Title  value.Optional[value.Nullable[string]] `json:"title,omitzero"`
	Budget value.Optional[int64]                  `json:"budget,omitzero"`
}

//foundry:dto
type ProjectView struct {
	ID     model.ID[Project]      `json:"id"`
	Slug   Slug                   `json:"slug"`
	Title  value.Nullable[string] `json:"title"`
	Budget int64                  `json:"budget"`
}

//foundry:dto
type SubmissionInput struct {
	Name   string `json:"name"`
	Reject bool   `json:"reject,omitempty"`
}

//foundry:dto
type SubmissionView struct {
	ID        model.ID[Submission] `json:"id"`
	ProjectID model.ID[Project]    `json:"project_id"`
	Name      string               `json:"name"`
}

//foundry:union name=Action discriminator=kind
type ActionVariants struct {
	Updated ProjectView    `union:"updated"`
	Queued  SubmissionView `union:"queued"`
}
type ActionEnvelope = genericdto.Envelope[Action]
type ProjectEnvelope = genericdto.Envelope[ProjectView]

// Public catalogue deliberately exposes only the project's opted-in slug.
//
//foundry:dto
type CatalogueItem struct {
	Slug Slug `json:"slug"`
}

//foundry:query
type CatalogueFilters struct {
	Team value.Optional[int64] `query:"team"`
}
