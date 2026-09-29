package attachments

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin   migrate.Origin  = "foundry.attachments"
	CreateAttachments migrate.ID      = "000001_create_attachments"
	CreateVariants    migrate.ID      = "000002_create_variants"
	Introduced        migrate.Version = "v0.1.0"
)

func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateAttachments}, Version: Introduced, SQL: []string{`CREATE TABLE foundry_attachments (
id uuid PRIMARY KEY,
owner text NOT NULL,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL CHECK (subject_key ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text)<=8192),
collection text NOT NULL,
locale text NOT NULL,
single boolean NOT NULL,
disk text NOT NULL,
object_key text NOT NULL,
original_name text NOT NULL CHECK (octet_length(original_name)<=1024),
content_type text NOT NULL,
size bigint NOT NULL CHECK (size BETWEEN 0 AND 67108864),
digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
etag text NOT NULL,
object_version text NOT NULL,
width integer NOT NULL CHECK (width>=0),
height integer NOT NULL CHECK (height>=0),
properties jsonb NOT NULL CHECK (octet_length(properties::text)<=262144),
sort_order integer NOT NULL CHECK (sort_order>=0),
state text NOT NULL CHECK (state IN ('writing','stored','ready','cleanup','cleaned','uncertain','retained')),
writer_id uuid NOT NULL,
attempts bigint NOT NULL CHECK (attempts BETWEEN 0 AND 4294967295),
last_failure text NOT NULL,
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
UNIQUE (disk,object_key),
CHECK (state NOT IN ('stored','ready','cleanup','retained') OR etag<>'')
)`, `CREATE UNIQUE INDEX foundry_attachments_single ON foundry_attachments (scope,subject_key,collection,locale) WHERE state='ready' AND single`,
		`CREATE INDEX foundry_attachments_collection ON foundry_attachments (scope,subject_key,collection,locale,state,sort_order,id)`,
		`CREATE INDEX foundry_attachments_reconcile ON foundry_attachments (state,updated_at,id)`,
		`CREATE INDEX foundry_attachments_owner ON foundry_attachments (owner,id)`}},
		// Variants are derived files of one attachment. A ready variant of each
		// name is unique per file; every other state is a retryable journal entry.
		{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateVariants}, Version: Introduced, SQL: []string{`CREATE TABLE foundry_attachment_variants (
id uuid PRIMARY KEY,
file_id uuid NOT NULL REFERENCES foundry_attachments(id),
name text NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9_.-]{0,63}$'),
disk text NOT NULL,
object_key text NOT NULL,
content_type text NOT NULL,
size bigint NOT NULL CHECK (size BETWEEN 0 AND 67108864),
digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
etag text NOT NULL,
object_version text NOT NULL,
width integer NOT NULL CHECK (width>=0),
height integer NOT NULL CHECK (height>=0),
state text NOT NULL CHECK (state IN ('writing','ready','cleanup','cleaned','uncertain')),
writer_id uuid NOT NULL,
attempts bigint NOT NULL CHECK (attempts BETWEEN 0 AND 4294967295),
last_failure text NOT NULL,
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
UNIQUE (disk,object_key),
CHECK (state NOT IN ('ready','cleanup') OR etag<>'')
)`, `CREATE UNIQUE INDEX foundry_attachment_variants_ready ON foundry_attachment_variants (file_id,name) WHERE state='ready'`,
			`CREATE INDEX foundry_attachment_variants_file ON foundry_attachment_variants (file_id,state,name)`,
			`CREATE INDEX foundry_attachment_variants_reconcile ON foundry_attachment_variants (state,updated_at,id)`}}}
}
