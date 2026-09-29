package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Hot paths and set-based maintenance use explicit schema-qualified SQL: the
// typed query layer has neither runtime schema qualification nor set-based
// deletes. Qualifying tables avoids a per-request transaction that only sets
// search_path, and temporary tables cannot shadow them. The schema passed the
// shared identifier validator at construction; every value is bound.
func (b *Backend) table(name string) string { return `"` + b.config.Schema + `".` + name }

func (b *Backend) familyTable() string     { return b.table("foundry_token_families") }
func (b *Backend) generationTable() string { return b.table("foundry_token_generations") }
func (b *Backend) consumedTable() string   { return b.table("foundry_token_consumed_refreshes") }

// dead is the SQL form of !token.Record.Live for family f and its current
// generation g at the bound instant at: expired absolutely, by refresh, or,
// without a refresh grant, by access expiry. It never evaluates to NULL, so
// NOT dead is equally safe.
func dead(at string) string {
	return `(f.expires_at <= ` + at + ` OR COALESCE(g.refresh_expires_at <= ` + at + `, g.access_expires_at <= ` + at + `))`
}

// selectTokens reads generation g with its family f and subject s in one
// statement. n is the family's current generation when g is the one before it,
// supplying the successor issue time that bounds the refresh grace.
func (b *Backend) selectTokens() string {
	return `SELECT g.id::text, g.generation, g.access_hash, g.refresh_hash, g.issued_at, g.last_seen_at, g.access_expires_at, g.refresh_expires_at, ` +
		`f.id::text, f.scope, f.subject_key, f.name, f.scopes::text, f.mode, f.assurance, f.access_nanos, f.refresh_idle_nanos, f.rotation_limit, f.generation, f.created_at, f.expires_at, f.client_ip, f.user_agent, ` +
		`s.key, s.scope, s.identity::text, n.issued_at FROM ` + b.generationTable() + ` g JOIN ` + b.familyTable() + ` f ON f.id = g.family_id AND f.scope = g.scope JOIN ` +
		b.table("foundry_token_subjects") + ` s ON s.key = f.subject_key AND s.scope = f.scope LEFT JOIN ` + b.generationTable() +
		` n ON n.family_id = f.id AND n.scope = f.scope AND n.generation = f.generation AND g.generation + 1 = f.generation`
}

type storedToken struct {
	subject tokenstore.Subject
	family  tokenstore.Family
	entry   tokenstore.Entry
	// successor is the current generation's issue time when entry precedes it.
	successor value.Optional[temporal.DateTime]
}

func instant(t time.Time) (temporal.DateTime, error) { return temporal.NewDateTime(t.UTC()) }

func unsigned32(v int64) (uint32, error) {
	if v < 0 || v > int64(token.MaxRotations) {
		return 0, fault.New(fault.Invalid, "stored token counter is out of range")
	}
	return uint32(v), nil
}

func scanToken(row database.Row) (storedToken, error) {
	var (
		result                                        storedToken
		entryID, familyID, scopes, identity           string
		generation, familyGeneration, rotation        int64
		mode, assurance                               int16
		issued, seen, accessExpires, created, expires time.Time
		refreshHash, client, agent                    sql.NullString
		refreshExpires, successor                     sql.NullTime
	)
	e, f, s := &result.entry, &result.family, &result.subject
	if err := row.Scan(&entryID, &generation, &e.AccessHash, &refreshHash, &issued, &seen, &accessExpires, &refreshExpires,
		&familyID, &f.Scope, &f.SubjectKey, &f.Name, &scopes, &mode, &assurance, &f.AccessNanos, &f.RefreshIdleNanos, &rotation, &familyGeneration, &created, &expires, &client, &agent,
		&s.Key, &s.Scope, &identity, &successor); err != nil {
		return storedToken{}, err
	}
	var err error
	if e.ID, err = model.ParseID[tokenstore.Entry](entryID); err != nil {
		return storedToken{}, err
	}
	if f.ID, err = model.ParseID[tokenstore.Family](familyID); err != nil {
		return storedToken{}, err
	}
	e.Scope, e.FamilyID = f.Scope, f.ID
	if e.Generation, err = unsigned32(generation); err != nil {
		return storedToken{}, err
	}
	if f.Generation, err = unsigned32(familyGeneration); err != nil {
		return storedToken{}, err
	}
	if f.RotationLimit, err = unsigned32(rotation); err != nil {
		return storedToken{}, err
	}
	if mode < 0 || mode > 255 || assurance < 0 || assurance > 255 {
		return storedToken{}, fault.New(fault.Invalid, "stored token mode or assurance is invalid")
	}
	f.Mode, f.Assurance = uint8(mode), uint8(assurance)
	for _, pair := range []struct {
		target *temporal.DateTime
		source time.Time
	}{{&e.IssuedAt, issued}, {&e.LastSeenAt, seen}, {&e.AccessExpiresAt, accessExpires}, {&f.CreatedAt, created}, {&f.ExpiresAt, expires}} {
		if *pair.target, err = instant(pair.source); err != nil {
			return storedToken{}, err
		}
	}
	if refreshHash.Valid {
		e.RefreshHash = value.Of(refreshHash.String)
	}
	if refreshExpires.Valid {
		at, err := instant(refreshExpires.Time)
		if err != nil {
			return storedToken{}, err
		}
		e.RefreshExpiresAt = value.Of(at)
	}
	if successor.Valid {
		at, err := instant(successor.Time)
		if err != nil {
			return storedToken{}, err
		}
		result.successor = value.Set(at)
	}
	if client.Valid {
		f.ClientIP = value.Of(client.String)
	}
	if agent.Valid {
		f.UserAgent = value.Of(agent.String)
	}
	if f.Scopes, err = value.ParseJSON[[]auth.AccessScopeName](scopes); err != nil {
		return storedToken{}, err
	}
	if s.Identity, err = value.ParseJSON[model.Identity](identity); err != nil {
		return storedToken{}, err
	}
	return result, nil
}

// readTokens streams at most limit joined rows; more rows is corruption.
func (b *Backend) readTokens(ctx context.Context, executor database.Executor, limit int, where string, arguments ...any) ([]storedToken, error) {
	var rows []storedToken
	err := database.ForEach(ctx, executor, b.selectTokens()+" "+where, arguments, scanToken, func(row storedToken) error {
		if len(rows) >= limit {
			return fault.New(fault.Invalid, "stored token rows exceed their bound")
		}
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// affected converts a statement's affected-row count to an unsigned total.
func affected(result database.Result) uint64 { return uint64(max(result.RowsAffected, 0)) }
