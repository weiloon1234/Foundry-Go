package teamworkflow

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// TeamTable declares the stored team fields. Example: Teams.Query().
type TeamTable struct {
	// Table owns the primary key. Example: orm.KeyEq(Teams.Key(), id).
	orm.Table[Team, int64]
	// Tenant binds the Tenant field. Example: orm.TextSet(Teams.Tenant, "tenant").
	Tenant orm.Text[Team, string]
	// Projects binds the Projects field. Example: Teams.Query().With(TeamProjects).
	Projects orm.Many[Team, Project]
}

// Teams preserves the persisted table and codecs. Example: Teams.Query().
var Teams = orm.Define("workflow_teams", func(b orm.Builder[Team], m *Team) TeamTable {
	return TeamTable{
		Table:    orm.TableOf(b, &m.ID),
		Tenant:   orm.TextOf(b, &m.Tenant),
		Projects: orm.ManyOf(b, &m.Projects),
	}
})

// ProjectTable declares the stored project fields. Example: Projects.Query().
type ProjectTable struct {
	// Table owns the primary key. Example: orm.KeyEq(Projects.Key(), id).
	orm.Table[Project, model.ID[Project]]
	// TeamID binds the TeamID field. Example: orm.NumberSet(Projects.TeamID, 1).
	TeamID orm.Number[Project, int64]
	// Slug binds the Slug field. Example: orm.TextSet(Projects.Slug, "shared").
	Slug orm.Text[Project, Slug]
	// Title binds the Title field. Example: orm.NullableTextSet(Projects.Title, "Title").
	Title orm.NullableText[Project, string]
	// Budget binds the Budget field. Example: orm.NumberSet(Projects.Budget, 0).
	Budget orm.Number[Project, int64]
	// Enabled binds the Enabled field. Example: orm.ColumnSet(Projects.Enabled, true).
	Enabled orm.Column[Project, bool]
}

// Projects preserves the persisted table and codecs. Example: Projects.Query().
var Projects = orm.Define("workflow_projects", func(b orm.Builder[Project], m *Project) ProjectTable {
	return ProjectTable{
		Table:   orm.TableOf(b, &m.ID),
		TeamID:  orm.NumberOf(b, &m.TeamID),
		Slug:    orm.TextOf(b, &m.Slug),
		Title:   orm.NullableTextOf(b, &m.Title),
		Budget:  orm.NumberOf(b, &m.Budget),
		Enabled: orm.ColumnOf(b, &m.Enabled),
	}
})

// SubmissionTable declares the stored submission fields. Example: Submissions.Query().
type SubmissionTable struct {
	// Table owns the primary key. Example: orm.KeyEq(Submissions.Key(), id).
	orm.Table[Submission, model.ID[Submission]]
	// ProjectID binds the ProjectID field. Example: orm.ColumnSet(Submissions.ProjectID, id).
	ProjectID orm.Column[Submission, model.ID[Project]]
	// Name binds the Name field. Example: orm.TextSet(Submissions.Name, "entry").
	Name orm.Text[Submission, string]
}

// Submissions preserves the persisted table and codecs. Example: Submissions.Query().
var Submissions = orm.Define("workflow_submissions", func(b orm.Builder[Submission], m *Submission) SubmissionTable {
	return SubmissionTable{
		Table:     orm.TableOf(b, &m.ID),
		ProjectID: orm.ColumnOf(b, &m.ProjectID),
		Name:      orm.TextOf(b, &m.Name),
	}
})

// TeamProjects scopes child lookup to its owning team. Example: Teams.Query().With(TeamProjects).
var TeamProjects = orm.HasManyOf(Teams.Projects, orm.KeyExpr(Teams.Key()), orm.NumberExpr(Projects.TeamID))

// Tables lists declarations for startup validation. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Teams, Projects, Submissions} }
