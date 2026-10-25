// Package teamworkflow is a complete, independently compiled typed API workflow.
package teamworkflow

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Slug string

type Team struct {
	ID       int64
	Tenant   string
	Projects relation.Many[Project]
}

type Project struct {
	ID      model.ID[Project]
	TeamID  int64
	Slug    Slug
	Title   value.Nullable[string]
	Budget  int64
	Enabled bool
}

type Submission struct {
	ID        model.ID[Submission]
	ProjectID model.ID[Project]
	Name      string
}
