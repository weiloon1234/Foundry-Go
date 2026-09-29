package postgres

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Hot paths and set-based maintenance use explicit schema-qualified SQL: the
// typed query layer has neither runtime schema qualification nor set-based
// deletes. Qualifying the table avoids a per-request transaction that only
// sets search_path, and temporary tables cannot shadow it. The schema passed
// the shared identifier validator at construction; every value is bound.
func (b *Backend) table(name string) string { return `"` + b.config.Schema + `".` + name }

func (b *Backend) sessions() string { return b.table("foundry_sessions") }

// selectEntries reads session rows with their subject in one statement. For an
// impersonation session it also reads the recorded actor session a (same
// table), whose liveness bounds the impersonation.
func (b *Backend) selectEntries() string {
	return `SELECT e.id::text, e.scope, e.subject_key, e.secret_hash, e.assurance, e.remember, e.sliding, e.idle_nanos, e.created_at, e.last_seen_at, e.idle_expires_at, e.expires_at, e.client_ip, e.user_agent, e.confirmed_at, e.impersonator_identity, e.impersonator_guard, e.impersonator_session::text, s.key, s.scope, s.identity::text, ` +
		`(a.id IS NOT NULL AND a.assurance = 2 AND a.impersonator_session IS NULL), a.idle_expires_at, a.expires_at FROM ` +
		b.sessions() + ` e JOIN ` + b.table("foundry_session_subjects") + ` s ON s.key = e.subject_key AND s.scope = e.scope LEFT JOIN ` +
		b.sessions() + ` a ON a.id = e.impersonator_session`
}

type storedSession struct {
	subject sessionstore.Subject
	entry   sessionstore.Entry
	// actor is the recorded actor session of an impersonation session.
	actorValid              bool
	actorIdle, actorExpires sql.NullTime
}

// actorLive reports whether an impersonation session's actor session still
// exists, is fully authenticated and live at now. Revoking the actor (logout,
// revoke-all, password reset) or its expiry ends the impersonation.
func (s storedSession) actorLive(now time.Time) bool {
	if _, impersonated := s.entry.ImpersonatorSession.Get(); !impersonated {
		return true
	}
	return s.actorValid && s.actorIdle.Valid && s.actorExpires.Valid && now.Before(s.actorIdle.Time) && now.Before(s.actorExpires.Time)
}

func instant(t time.Time) (temporal.DateTime, error) { return temporal.NewDateTime(t.UTC()) }

func scanSession(row database.Row) (storedSession, error) {
	var (
		result                       storedSession
		id, identity                 string
		assurance                    int16
		created, seen, idle, expires time.Time
		client, agent                sql.NullString
		confirmed                    sql.NullTime
		impersonator, guard, actor   sql.NullString
	)
	e := &result.entry
	if err := row.Scan(&id, &e.Scope, &e.SubjectKey, &e.SecretHash, &assurance, &e.Remember, &e.Sliding, &e.IdleNanos, &created, &seen, &idle, &expires, &client, &agent, &confirmed, &impersonator, &guard, &actor, &result.subject.Key, &result.subject.Scope, &identity, &result.actorValid, &result.actorIdle, &result.actorExpires); err != nil {
		return storedSession{}, err
	}
	parsed, err := model.ParseID[sessionstore.Entry](id)
	if err != nil {
		return storedSession{}, err
	}
	if assurance < 0 || assurance > 255 {
		return storedSession{}, fault.New(fault.Invalid, "stored session assurance is invalid")
	}
	e.ID, e.Assurance = parsed, uint8(assurance)
	for _, pair := range []struct {
		target *temporal.DateTime
		source time.Time
	}{{&e.CreatedAt, created}, {&e.LastSeenAt, seen}, {&e.IdleExpiresAt, idle}, {&e.ExpiresAt, expires}} {
		if *pair.target, err = instant(pair.source); err != nil {
			return storedSession{}, err
		}
	}
	if client.Valid {
		e.ClientIP = value.Of(client.String)
	}
	if agent.Valid {
		e.UserAgent = value.Of(agent.String)
	}
	if confirmed.Valid {
		at, err := instant(confirmed.Time)
		if err != nil {
			return storedSession{}, err
		}
		e.ConfirmedAt = value.Of(at)
	}
	if impersonator.Valid {
		e.ImpersonatorIdentity = value.Of(impersonator.String)
	}
	if guard.Valid {
		e.ImpersonatorGuard = value.Of(guard.String)
	}
	if actor.Valid {
		id, err := model.ParseID[sessionstore.Entry](actor.String)
		if err != nil {
			return storedSession{}, err
		}
		e.ImpersonatorSession = value.Of(id)
	}
	result.subject.Identity, err = value.ParseJSON[model.Identity](identity)
	if err != nil {
		return storedSession{}, err
	}
	return result, nil
}

// readSessions streams at most limit joined rows; more rows is corruption.
func (b *Backend) readSessions(ctx context.Context, executor database.Executor, limit int, where string, arguments ...any) ([]storedSession, error) {
	var rows []storedSession
	err := database.ForEach(ctx, executor, b.selectEntries()+" "+where, arguments, scanSession, func(row storedSession) error {
		if len(rows) >= limit {
			return fault.New(fault.Invalid, "stored session rows exceed their bound")
		}
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// deviceColumns returns bound device values; zero metadata is stored as NULL.
func deviceColumns(r session.Record) (sql.NullString, sql.NullString) {
	var client, agent sql.NullString
	if r.Device.ClientIP.IsValid() {
		client = sql.NullString{String: r.Device.ClientIP.String(), Valid: true}
	}
	if r.Device.UserAgent != "" {
		agent = sql.NullString{String: r.Device.UserAgent, Valid: true}
	}
	return client, agent
}

// impersonatorColumns returns bound impersonation values; absent is NULL.
func impersonatorColumns(r session.Record) (sql.NullString, sql.NullString, sql.NullString, error) {
	var identity, guard, actor sql.NullString
	impersonator, present := r.Impersonator.Get()
	if !present {
		return identity, guard, actor, nil
	}
	encoded, err := value.NewJSON(impersonator.Subject)
	if err != nil {
		return identity, guard, actor, err
	}
	text, err := encoded.MarshalJSON()
	if err != nil {
		return identity, guard, actor, err
	}
	return sql.NullString{String: string(text), Valid: true}, sql.NullString{String: string(impersonator.Guard), Valid: true}, sql.NullString{String: impersonator.Session.String(), Valid: true}, nil
}

// uuids renders identifiers for a bound uuid[] parameter.
func uuids(ids []model.ID[sessionstore.Entry]) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = id.String()
	}
	return result
}

func joinSQL(parts ...string) string { return strings.Join(parts, " ") }
