package enumlists

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	CreateDocumentsID migrate.ID = "202611250001_create_enum_list_documents"
	ArchivedStatusID  migrate.ID = "202611250002_enum_list_documents_archived"
)

// CreateDocuments writes the first lists of the enum columns.
func CreateDocuments() migrate.Definition {
	return migrate.Definition{
		Key:     migrate.Key{Origin: "consumer.enumlists", ID: CreateDocumentsID},
		Version: "v0.1.0",
		SQL: []string{
			`CREATE TABLE enum_list_documents (
	id uuid PRIMARY KEY,
	title text NOT NULL,
	status text NOT NULL CONSTRAINT enum_list_documents_status_check CHECK (status IN ('draft', 'published')),
	roles jsonb NOT NULL DEFAULT '[]'::jsonb,
	stage text NOT NULL CHECK (stage IN ('draft', 'published')),
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	CONSTRAINT enum_list_documents_roles_check CHECK (jsonb_typeof(roles) = 'array' AND (roles = '[]'::jsonb OR roles <@ '["reader","editor"]'::jsonb))
)`,
		},
	}
}

// doc-example:begin enum-list-new-migration

// ArchivedStatus widens the status list after Archived joined the enum. The
// first migration stays as it was applied; this one drops the constraint and
// writes it again with every case as a literal.
func ArchivedStatus() migrate.Definition {
	return migrate.Definition{
		Key:      migrate.Key{Origin: "consumer.enumlists", ID: ArchivedStatusID},
		Version:  "v0.1.0",
		Requires: []migrate.Key{CreateDocuments().Key},
		SQL: []string{
			`ALTER TABLE enum_list_documents DROP CONSTRAINT enum_list_documents_status_check`,
			`ALTER TABLE enum_list_documents ADD CONSTRAINT enum_list_documents_status_check CHECK (status IN ('draft', 'published', 'archived'))`,
		},
	}
}

// doc-example:end enum-list-new-migration

// Definitions lists every migration of the package.
func Definitions() []migrate.Definition {
	return []migrate.Definition{CreateDocuments(), ArchivedStatus()}
}
