package audit

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.audit"
	CreateEntries   migrate.ID      = "000001_create_entries"
	AddHistoryKeys  migrate.ID      = "000002_add_history_keys"
	Introduced      migrate.Version = "v0.1.0"
)

// identityKeySQL mirrors identityKey: md5 of "m:<model>:<kind>:<text>" over the
// stored model identity JSON. It is an index key, not a security digest.
func identityKeySQL(column string) string {
	return `md5('m:' || (` + column + `->>'model') || ':' || (` + column + `->'key'->>'k') || ':' || (` + column + `->'key'->>'t'))`
}

// Migrations returns owned historical definitions. Register and run them with
// the ordinary explicit migration command against the recorder's database/schema.
// Missing storage fails enabled audit writes instead of silently discarding them.
//
// AddHistoryKeys rewrites the table once: it assigns every existing row an
// insertion sequence in (created_at, id) order, records the original redaction
// policy, backfills request correlation and adds database-generated subject and
// actor lookup keys with their history indexes. The lookup keys hash UTF-8 text,
// so the migration requires a UTF8 (or SQL_ASCII) database encoding.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{
		Key:     migrate.Key{Origin: MigrationOrigin, ID: CreateEntries},
		Version: Introduced,
		SQL: []string{
			`CREATE TABLE foundry_audit (
id uuid PRIMARY KEY,
area text NOT NULL CHECK (octet_length(area) BETWEEN 1 AND 128),
operation smallint NOT NULL,
action text NOT NULL,
version bigint NOT NULL CHECK (version BETWEEN 0 AND 4294967295),
subject jsonb,
payload jsonb NOT NULL,
redacted boolean NOT NULL,
origin jsonb NOT NULL,
created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
CHECK ((operation BETWEEN 1 AND 6 AND action = '' AND version = 0 AND subject IS NOT NULL AND NOT redacted)
    OR (operation = 0 AND octet_length(action) BETWEEN 1 AND 128 AND version > 0))
)`,
			`CREATE INDEX foundry_audit_order ON foundry_audit (area, created_at, id)`,
			`CREATE INDEX foundry_audit_subject ON foundry_audit USING HASH (subject)`,
		},
	}, {
		Key:     migrate.Key{Origin: MigrationOrigin, ID: AddHistoryKeys},
		Version: Introduced,
		SQL: []string{
			`DO $$
BEGIN
	IF current_setting('server_encoding') NOT IN ('UTF8', 'SQL_ASCII') THEN
		RAISE EXCEPTION 'foundry_audit history keys require a UTF8 database encoding';
	END IF;
END
$$`,
			`ALTER TABLE foundry_audit
ADD COLUMN sequence bigint,
ADD COLUMN redaction smallint NOT NULL DEFAULT 1 CHECK (redaction BETWEEN 1 AND 255),
ADD COLUMN correlation text CHECK (octet_length(correlation) BETWEEN 1 AND 128),
ADD COLUMN request_method text NOT NULL DEFAULT '' CHECK (octet_length(request_method) <= 16),
ADD COLUMN request_route text NOT NULL DEFAULT '' CHECK (octet_length(request_route) <= 256),
ADD COLUMN subject_key text GENERATED ALWAYS AS (` + identityKeySQL("subject") + `) STORED,
ADD COLUMN actor_key text GENERATED ALWAYS AS (CASE
    WHEN origin->'subject' IS NOT NULL THEN ` + identityKeySQL("(origin->'subject')") + `
    WHEN origin->>'system' IS NOT NULL THEN md5('s:' || (origin->>'system'))
END) STORED`,
			`UPDATE foundry_audit AS entry
SET sequence = ordered.position, correlation = NULLIF(entry.origin->'request'->>'id', '')
FROM (SELECT id, row_number() OVER (ORDER BY created_at, id) AS position FROM foundry_audit) AS ordered
WHERE entry.id = ordered.id`,
			`ALTER TABLE foundry_audit
ALTER COLUMN sequence SET NOT NULL,
ALTER COLUMN sequence ADD GENERATED ALWAYS AS IDENTITY`,
			`SELECT setval(pg_get_serial_sequence('foundry_audit', 'sequence'), COALESCE((SELECT max(sequence) FROM foundry_audit), 0) + 1, false)`,
			`CREATE INDEX foundry_audit_subject_history ON foundry_audit (area, subject_key, sequence) WHERE subject_key IS NOT NULL`,
			`CREATE INDEX foundry_audit_actor_history ON foundry_audit (area, actor_key, sequence) WHERE actor_key IS NOT NULL`,
			`CREATE INDEX foundry_audit_correlation ON foundry_audit (area, correlation, sequence) WHERE correlation IS NOT NULL`,
			`CREATE INDEX foundry_audit_action_history ON foundry_audit (area, action, version, sequence)`,
			`DROP INDEX foundry_audit_subject`,
		},
	}}
}
