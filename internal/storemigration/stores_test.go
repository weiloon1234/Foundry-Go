package storemigration_test

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
	"github.com/weiloon1234/Foundry-Go/internal/idempotencystore"
	"github.com/weiloon1234/Foundry-Go/internal/jobarchive"
	"github.com/weiloon1234/Foundry-Go/internal/mfastore"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

var instant = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

const uuid = "01890f47-1a00-7000-8000-000000000001"

type statement struct {
	SQL  string
	Args []any
}
type capture struct {
	Statements                 []statement
	Begins, Commits, Rollbacks int
	Result                     string
	values                     []driver.Value
	columns                    []string
}
type connector struct{ *capture }

func (c connector) Driver() driver.Driver                                        { return c }
func (c connector) Connect(context.Context) (driver.Conn, error)                 { return c, nil }
func (c connector) Open(string) (driver.Conn, error)                             { return c, nil }
func (c connector) Prepare(string) (driver.Stmt, error)                          { return nil, errors.New("unexpected prepare") }
func (c connector) Close() error                                                 { return nil }
func (c connector) Ping(context.Context) error                                   { return nil }
func (c connector) Begin() (driver.Tx, error)                                    { c.Begins++; return transaction{c.capture}, nil }
func (c connector) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) { return c.Begin() }

type transaction struct{ *capture }

func (t transaction) Commit() error   { t.Commits++; return nil }
func (t transaction) Rollback() error { t.Rollbacks++; return nil }
func (c connector) record(sql string, args []driver.NamedValue) {
	values := make([]any, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	c.Statements = append(c.Statements, statement{sql, values})
}
func (c connector) ExecContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Result, error) {
	c.record(sql, args)
	return driver.RowsAffected(1), nil
}
func (c connector) QueryContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	c.record(sql, args)
	return &resultRows{values: c.values, columns: c.columns}, nil
}

type resultRows struct {
	values  []driver.Value
	columns []string
	done    bool
}

func (r *resultRows) Columns() []string { return r.columns }
func (r *resultRows) Close() error      { return nil }
func (r *resultRows) Next(v []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(v, r.values)
	return nil
}
func fingerprint(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(data)), err
}

// Goldens are captured against the branch-base generated APIs using a Go source
// overlay. SQL, bindings, hydration and transaction commands are compared exactly.
func TestStoreMigrationParity(t *testing.T) {
	cases := []struct {
		name    string
		columns []string
		values  []driver.Value
		run     func(context.Context, *database.Tx) (string, error)
	}{
		{name: "countries_country_read", columns: []string{"iso2", "iso3", "iso_numeric", "name", "official_name", "capital", "region", "subregion", "currencies", "primary_currency_code", "calling_code", "calling_root", "calling_suffixes", "tlds", "timezones", "latitude", "longitude", "independent", "un_member", "flag_emoji", "status", "conversion_rate", "is_default", "reference_version", "created_at", "updated_at"}, values: []driver.Value{"MY", "stored", nil, "stored", nil, nil, nil, nil, `[]`, nil, nil, nil, `[]`, `[]`, `[]`, nil, nil, nil, nil, nil, "disabled", nil, false, "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := countries.Countries.Query().Where(countries.Countries.ISO2.Eq(countries.Code("MY"))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "countries_country_write", columns: []string{"iso2", "iso3", "iso_numeric", "name", "official_name", "capital", "region", "subregion", "currencies", "primary_currency_code", "calling_code", "calling_root", "calling_suffixes", "tlds", "timezones", "latitude", "longitude", "independent", "un_member", "flag_emoji", "status", "conversion_rate", "is_default", "reference_version", "created_at", "updated_at"}, values: []driver.Value{"MY", "stored", nil, "stored", nil, nil, nil, nil, `[]`, nil, nil, nil, `[]`, `[]`, `[]`, nil, nil, nil, nil, nil, "disabled", nil, false, "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, countries.Countries.Query(), countries.Countries.ISO2, countries.Code("MY"), countries.Countries.Name.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "jobarchive_failure_read", columns: []string{"id", "execution", "queue", "name", "version", "envelope", "reason", "attempts", "exceptions", "retries", "failed_at", "retried_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", int64(1), "stored", "stored", int64(1), int64(1), int64(1), instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := jobarchive.Failures.Query().Where(jobarchive.Failures.ID.Eq(model.IDFromBytes[jobarchive.Failure]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "jobarchive_failure_write", columns: []string{"id", "execution", "queue", "name", "version", "envelope", "reason", "attempts", "exceptions", "retries", "failed_at", "retried_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", int64(1), "stored", "stored", int64(1), int64(1), int64(1), instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, jobarchive.Failures.Query(), jobarchive.Failures.ID, model.IDFromBytes[jobarchive.Failure]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), jobarchive.Failures.Reason.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "idempotencystore_record_read", columns: []string{"id", "namespace", "operation", "version", "scope_digest", "key_digest", "fingerprint", "result_schema", "result_hash", "representation", "completed_at", "expires_at"}, values: []driver.Value{uuid, "stored", "stored", int64(1), "stored", "stored", "stored", "stored", "stored", []byte("stored"), nil, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := idempotencystore.Records.Query().Where(idempotencystore.Records.ID.Eq(model.IDFromBytes[idempotencystore.Record]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "idempotencystore_record_write", columns: []string{"id", "namespace", "operation", "version", "scope_digest", "key_digest", "fingerprint", "result_schema", "result_hash", "representation", "completed_at", "expires_at"}, values: []driver.Value{uuid, "stored", "stored", int64(1), "stored", "stored", "stored", "stored", "stored", []byte("stored"), nil, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, idempotencystore.Records.Query(), idempotencystore.Records.ID, model.IDFromBytes[idempotencystore.Record]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), idempotencystore.Records.ResultHash.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "mfastore_factor_read", columns: []string{"key", "scope", "identity", "generation", "ciphertext", "created_at", "pending_until", "confirmed_at", "last_step", "recovery_hashes"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, uuid, "stored", instant, nil, nil, nil, `[]`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := mfastore.Factors.Query().Where(mfastore.Factors.Key().Eq("subject")).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "mfastore_factor_write", columns: []string{"key", "scope", "identity", "generation", "ciphertext", "created_at", "pending_until", "confirmed_at", "last_step", "recovery_hashes"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, uuid, "stored", instant, nil, nil, nil, `[]`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, mfastore.Factors.Query(), mfastore.Factors.Key(), "subject", mfastore.Factors.Ciphertext.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "challengestore_subject_read", columns: []string{"key", "scope", "identity"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := challengestore.Subjects.Query().Where(challengestore.Subjects.Key().Eq("subject")).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "challengestore_subject_write", columns: []string{"key", "scope", "identity"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Upsert(ctx, tx, challengestore.Subjects.Query(), orm.OnConflict(orm.ConflictKey(challengestore.Subjects.Key())).DoNothing(), challengestore.Subjects.Key().Set("subject"), challengestore.Subjects.Scope.Set("stored"), challengestore.Subjects.Identity.Set(mustIdentity(t)))
			return fingerprint(r, e)
		}},
		{name: "challengestore_entry_read", columns: []string{"id", "scope", "subject_key", "secret_hash", "binding_hash", "created_at", "expires_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := challengestore.Entries.Query().Where(challengestore.Entries.ID.Eq(model.IDFromBytes[challengestore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "challengestore_entry_write", columns: []string{"id", "scope", "subject_key", "secret_hash", "binding_hash", "created_at", "expires_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, challengestore.Entries.Query(), challengestore.Entries.ID, model.IDFromBytes[challengestore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), challengestore.Entries.SecretHash.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "sessionstore_subject_read", columns: []string{"key", "scope", "identity"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := sessionstore.Subjects.Query().Where(sessionstore.Subjects.Key().Eq("subject")).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "sessionstore_subject_write", columns: []string{"key", "scope", "identity"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Upsert(ctx, tx, sessionstore.Subjects.Query(), orm.OnConflict(orm.ConflictKey(sessionstore.Subjects.Key())).DoNothing(), sessionstore.Subjects.Key().Set("subject"), sessionstore.Subjects.Scope.Set("stored"), sessionstore.Subjects.Identity.Set(mustIdentity(t)))
			return fingerprint(r, e)
		}},
		{name: "sessionstore_entry_read", columns: []string{"id", "scope", "subject_key", "secret_hash", "assurance", "remember", "sliding", "idle_nanos", "created_at", "last_seen_at", "idle_expires_at", "expires_at", "client_ip", "user_agent", "confirmed_at", "impersonator_identity", "impersonator_guard", "impersonator_session"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), false, false, int64(1), instant, instant, instant, instant, nil, nil, nil, nil, nil, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := sessionstore.Entries.Query().Where(sessionstore.Entries.ID.Eq(model.IDFromBytes[sessionstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "sessionstore_entry_write", columns: []string{"id", "scope", "subject_key", "secret_hash", "assurance", "remember", "sliding", "idle_nanos", "created_at", "last_seen_at", "idle_expires_at", "expires_at", "client_ip", "user_agent", "confirmed_at", "impersonator_identity", "impersonator_guard", "impersonator_session"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), false, false, int64(1), instant, instant, instant, instant, nil, nil, nil, nil, nil, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, sessionstore.Entries.Query(), sessionstore.Entries.ID, model.IDFromBytes[sessionstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), sessionstore.Entries.SecretHash.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "tokenstore_subject_read", columns: []string{"key", "scope", "identity"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := tokenstore.Subjects.Query().Where(tokenstore.Subjects.Key().Eq("subject")).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "tokenstore_subject_write", columns: []string{"key", "scope", "identity"}, values: []driver.Value{"subject", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Upsert(ctx, tx, tokenstore.Subjects.Query(), orm.OnConflict(orm.ConflictKey(tokenstore.Subjects.Key())).DoNothing(), tokenstore.Subjects.Key().Set("subject"), tokenstore.Subjects.Scope.Set("stored"), tokenstore.Subjects.Identity.Set(mustIdentity(t)))
			return fingerprint(r, e)
		}},
		{name: "tokenstore_family_read", columns: []string{"id", "scope", "subject_key", "name", "scopes", "mode", "assurance", "access_nanos", "refresh_idle_nanos", "rotation_limit", "generation", "created_at", "expires_at", "client_ip", "user_agent"}, values: []driver.Value{uuid, "stored", "stored", "stored", `[]`, int64(1), int64(1), int64(1), int64(1), int64(1), int64(1), instant, instant, nil, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := tokenstore.Families.Query().Where(tokenstore.Families.ID.Eq(model.IDFromBytes[tokenstore.Family]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "tokenstore_family_write", columns: []string{"id", "scope", "subject_key", "name", "scopes", "mode", "assurance", "access_nanos", "refresh_idle_nanos", "rotation_limit", "generation", "created_at", "expires_at", "client_ip", "user_agent"}, values: []driver.Value{uuid, "stored", "stored", "stored", `[]`, int64(1), int64(1), int64(1), int64(1), int64(1), int64(1), instant, instant, nil, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, tokenstore.Families.Query(), tokenstore.Families.ID, model.IDFromBytes[tokenstore.Family]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), tokenstore.Families.Name.Set("updated"))
			return fingerprint(r, e)
		}},
		{name: "tokenstore_entry_read", columns: []string{"id", "scope", "family_id", "generation", "access_hash", "refresh_hash", "issued_at", "last_seen_at", "access_expires_at", "refresh_expires_at"}, values: []driver.Value{uuid, "stored", uuid, int64(1), "stored", nil, instant, instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := tokenstore.Entries.Query().Where(tokenstore.Entries.ID.Eq(model.IDFromBytes[tokenstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(r, e)
		}},
		{name: "tokenstore_entry_write", columns: []string{"id", "scope", "family_id", "generation", "access_hash", "refresh_hash", "issued_at", "last_seen_at", "access_expires_at", "refresh_expires_at"}, values: []driver.Value{uuid, "stored", uuid, int64(1), "stored", nil, instant, instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			r, e := orm.Update(ctx, tx, tokenstore.Entries.Query(), tokenstore.Entries.ID, model.IDFromBytes[tokenstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), tokenstore.Entries.AccessHash.Set("updated"))
			return fingerprint(r, e)
		}},

		{name: "challenge_prune_projection", columns: []string{"id", "subject_key"}, values: []driver.Value{uuid, "subject"}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			at, _ := temporal.NewDateTime(instant)
			f := challengestore.Entries
			q := f.Query().Where(f.Scope.Eq("scope"), f.ExpiresAt.Lte(at)).OrderBy(f.SubjectKey.Asc(), f.ID.Asc()).Limit(10)
			r, e := rows.Read(rows.Select(rows.From(q), challengestore.PruneCandidateRows, rows.Map(challengestore.PruneCandidateRows.ID, expr.Value(f.ID)), rows.Map(challengestore.PruneCandidateRows.SubjectKey, expr.Value(f.SubjectKey)))).All(ctx, tx)
			return fingerprint(r, e)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &capture{columns: tc.columns, values: tc.values}
			db, err := database.Open(t.Context(), database.Adapter{Connector: connector{c}}, database.DefaultPoolConfig(), database.WithClock(testkit.NewClock(instant)))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			err = db.Transaction(t.Context(), func(tx *database.Tx) error { var e error; c.Result, e = tc.run(t.Context(), tx); return e })
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, s := range c.Statements {
				if strings.HasPrefix(s.SQL, "SELECT") || strings.HasPrefix(s.SQL, "UPDATE") || strings.HasPrefix(s.SQL, "INSERT") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("statements %d, want 1", count)
			}
			data, e := json.MarshalIndent(c, "", "  ")
			if e != nil {
				t.Fatal(e)
			}
			data = append(data, '\n')
			path := filepath.Join("testdata", tc.name+".json")
			if os.Getenv("FOUNDRY_STORE_ORACLE") == "1" {
				if e = os.WriteFile(path, data, 0644); e != nil {
					t.Fatal(e)
				}
				return
			}
			want, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(data, want) {
				t.Fatalf("store transcript changed:\n%s\nwant:\n%s", data, want)
			}
		})
	}
}
func mustIdentity(t *testing.T) value.JSON[model.Identity] {
	t.Helper()
	v, e := value.ParseJSON[model.Identity](`{"model":"subjects","key":{"k":"int","t":"7"}}`)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
