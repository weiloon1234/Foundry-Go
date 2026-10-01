package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ token.TicketBackend = (*Backend)(nil)

func (b *Backend) ticketTable() string { return b.table("foundry_token_tickets") }

// liveFamily joins family f with its current generation g and subject s for the
// single-statement ticket paths.
func (b *Backend) liveFamily() string {
	return b.familyTable() + ` f JOIN ` + b.generationTable() + ` g ON g.family_id = f.id AND g.scope = f.scope AND g.generation = f.generation`
}

func (b *Backend) ready(ctx context.Context) error {
	if b == nil || b.db == nil || ctx == nil {
		return fault.New(fault.Invalid, "token PostgreSQL operation requires a backend and context")
	}
	return nil
}

// IssueTicket stores a ticket hash. It first locks the live family row, then,
// in a later statement whose snapshot sees every ticket committed by earlier
// lock holders, drops the family's expired tickets and the oldest beyond max-1
// and inserts, so concurrent issuance for one family keeps the cap exact. A
// family that is revoked, expired or another subject's inserts nothing.
func (b *Backend) IssueTicket(ctx context.Context, address token.Address, identity model.Identity, family model.ID[token.Record], hash token.Digest, lifetime time.Duration, max int) (value.Optional[temporal.DateTime], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	if family.IsZero() || lifetime < time.Second || lifetime > token.MaxTicketLifetime || max < 1 || max > token.MaxFamilyTickets {
		return value.Optional[temporal.DateTime]{}, fault.New(fault.Invalid, "invalid token ticket issuance")
	}
	if err := b.ready(ctx); err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	scope, err := address.Key()
	if err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	now, err := b.now()
	if err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	tickets := b.ticketTable()
	live := b.liveFamily() + ` WHERE f.id = $1 AND f.scope = $2 AND f.subject_key = $3 AND NOT ` + dead("$4") + ` FOR NO KEY UPDATE OF f`
	var expires time.Time
	present := false
	err = b.within(ctx, func(tx *database.Tx) error {
		// A single statement's snapshot predates its lock wait and would miss a
		// concurrent issuer's ticket; READ COMMITTED gives the next one a fresh view.
		var locked string
		if err := database.ScanOne(ctx, tx, `SELECT f.id::text FROM `+live, []any{family.String(), scope, key, now}, &locked); err != nil {
			if errors.Is(err, database.NotFound) {
				return nil
			}
			return err
		}
		err := database.ScanOne(ctx, tx, `WITH live AS (SELECT f.id FROM `+live+`), `+
			`dropped AS (DELETE FROM `+tickets+` t WHERE t.scope = $2 AND t.family_id IN (SELECT id FROM live) AND (t.expires_at <= $4 OR t.ticket_hash IN `+
			`(SELECT k.ticket_hash FROM `+tickets+` k WHERE k.scope = $2 AND k.family_id = $1 AND k.expires_at > $4 ORDER BY k.created_at DESC, k.ticket_hash OFFSET $7))) `+
			`INSERT INTO `+tickets+` (ticket_hash, scope, family_id, created_at, expires_at) SELECT $5, $2, id, $4, $6 FROM live RETURNING expires_at`,
			[]any{family.String(), scope, key, now, hash.Hex(), now.Add(lifetime), max - 1}, &expires)
		if errors.Is(err, database.NotFound) {
			return nil
		}
		present = err == nil
		return err
	})
	if err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	if !present {
		return value.Optional[temporal.DateTime]{}, nil
	}
	at, err := instant(expires)
	if err != nil {
		return value.Optional[temporal.DateTime]{}, err
	}
	return value.Set(at), nil
}

// RedeemTicket deletes the ticket in the statement that reads it, so concurrent
// redemptions have at most one success. A ticket of a family that is no longer
// live is left for pruning and reported absent, like an expired one.
func (b *Backend) RedeemTicket(ctx context.Context, address token.Address, hash token.Digest) (value.Optional[token.TicketRecord], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	if err := b.ready(ctx); err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	scope, err := address.Key()
	if err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	now, err := b.now()
	if err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	var family, identity string
	var expires time.Time
	err = database.ScanOne(ctx, b.db, `DELETE FROM `+b.ticketTable()+` t USING `+b.liveFamily()+` JOIN `+b.table("foundry_token_subjects")+` s ON s.key = f.subject_key AND s.scope = f.scope `+
		`WHERE t.scope = $1 AND t.ticket_hash = $2 AND t.expires_at > $3 AND f.id = t.family_id AND f.scope = t.scope AND NOT `+dead("$3")+
		` RETURNING t.family_id::text, s.identity::text, t.expires_at`, []any{scope, hash.Hex(), now}, &family, &identity, &expires)
	if errors.Is(err, database.NotFound) {
		return value.Optional[token.TicketRecord]{}, nil
	}
	if err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	var result token.TicketRecord
	if result.Family, err = model.ParseID[token.Record](family); err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	stored, err := value.ParseJSON[model.Identity](identity)
	if err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	if result.Subject, err = stored.Decode(); err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	if result.ExpiresAt, err = instant(expires); err != nil {
		return value.Optional[token.TicketRecord]{}, err
	}
	return value.Set(result), nil
}

// LookupFamily reads the family's current generation in one statement without
// a lock, like request authentication, and omits a family that is not live.
func (b *Backend) LookupFamily(ctx context.Context, address token.Address, identity model.Identity, family model.ID[token.Record]) (value.Optional[token.Record], error) {
	if family.IsZero() {
		return value.Optional[token.Record]{}, fault.New(fault.Invalid, "token family lookup requires an identifier")
	}
	if err := b.ready(ctx); err != nil {
		return value.Optional[token.Record]{}, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	scope, err := address.Key()
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	rows, err := b.readTokens(ctx, b.db, 1, `WHERE f.id = $1 AND f.scope = $2 AND f.subject_key = $3 AND g.generation = f.generation`, family.String(), scope, key)
	if err != nil || len(rows) == 0 {
		return value.Optional[token.Record]{}, err
	}
	current, err := record(address, rows[0].subject, rows[0].family, rows[0].entry)
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	now, err := b.now()
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	if !current.Live(now) {
		return value.Optional[token.Record]{}, nil
	}
	return value.Set(current), nil
}

// PruneTickets deletes at most limit expired tickets of this address. The
// candidate set is materialized once so its LIMIT bounds the delete.
func (b *Backend) PruneTickets(ctx context.Context, address token.Address, limit int) (uint64, error) {
	if limit < 1 || limit > token.MaxPruneFamilies {
		return 0, fault.New(fault.Invalid, "invalid token ticket prune limit")
	}
	if err := b.ready(ctx); err != nil {
		return 0, err
	}
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	now, err := b.now()
	if err != nil {
		return 0, err
	}
	tickets := b.ticketTable()
	result, err := b.db.Exec(ctx, `WITH doomed AS MATERIALIZED (SELECT k.ticket_hash FROM `+tickets+` k WHERE k.scope = $1 AND k.expires_at <= $2 ORDER BY k.expires_at LIMIT $3) `+
		`DELETE FROM `+tickets+` t USING doomed WHERE t.scope = $1 AND t.ticket_hash = doomed.ticket_hash`, scope, now, limit)
	if err != nil {
		return 0, err
	}
	return affected(result), nil
}
