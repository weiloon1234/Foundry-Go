package teamworkflow

import "github.com/weiloon1234/Foundry-Go/database/migrate"

func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: "consumer.workflow", ID: "001_domain"}, Version: "v1", SQL: []string{
		`CREATE TABLE workflow_teams(id bigint PRIMARY KEY,tenant text NOT NULL)`,
		`CREATE TABLE workflow_projects(id uuid PRIMARY KEY,team_id bigint NOT NULL REFERENCES workflow_teams(id),slug text NOT NULL,title text NULL,budget bigint NOT NULL,enabled boolean NOT NULL,UNIQUE(team_id,slug))`,
		`CREATE TABLE workflow_submissions(id uuid PRIMARY KEY,project_id uuid NOT NULL REFERENCES workflow_projects(id),name text NOT NULL)`,
		`INSERT INTO workflow_teams(id,tenant) VALUES(1,'tenant-a'),(2,'tenant-b')`,
		`INSERT INTO workflow_projects(id,team_id,slug,title,budget,enabled) VALUES('0193fd8c-2075-7000-8000-000000000001',1,'shared','Initial',10,true),('0193fd8c-2075-7000-8000-000000000002',2,'shared','Other',99,true),('0193fd8c-2075-7000-8000-000000000003',2,'foreign','Foreign',99,true)`,
	}}}
}
func ReceiverMigrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: "consumer.workflow.receiver", ID: "001_receipts"}, Version: "v1", SQL: []string{
		`CREATE TABLE workflow_delivery_receipts(id uuid PRIMARY KEY,payload jsonb NOT NULL)`,
		`CREATE TABLE workflow_delivery_totals(name text PRIMARY KEY,total bigint NOT NULL)`,
		`INSERT INTO workflow_delivery_totals(name,total) VALUES('submissions',0)`,
	}}}
}
