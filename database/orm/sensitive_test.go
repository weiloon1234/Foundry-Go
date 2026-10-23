package orm

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
	"log/slog"
	"strings"
	"testing"
)

type secureRecord struct {
	ID     model.ID[secureRecord]
	Secret encrypted.Text
	Backup value.Nullable[encrypted.Text]
}
type secureTable struct {
	Table[secureRecord, model.ID[secureRecord]]
	Secret Encrypted[secureRecord, encrypted.Text]
	Backup NullableEncrypted[secureRecord, encrypted.Text]
}

func secureRecords() secureTable { return secureRecordsAt("secure_records") }
func secureRecordsAt(name string) secureTable {
	return Define(name, func(b Builder[secureRecord], m *secureRecord) secureTable {
		return secureTable{TableOf(b, &m.ID), EncryptedTextOf(b, &m.Secret), NullableEncryptedTextOf(b, &m.Backup)}
	})
}
func sensitiveKeys(t *testing.T) *encryption.Keyring {
	t.Helper()
	key, err := encryption.GenerateKey("field_test")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := encryption.NewKeyring("field_test", key)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}
func TestEncryptedWriteBoundaries(t *testing.T) {
	r := secureRecords()
	if err := Check(r); err != nil {
		t.Fatal(err)
	}
	var statements []string
	var stored []driver.Value
	begins := 0
	ring := sensitiveKeys(t)
	db := testDB(t, testConnector{statements: &statements, onBegin: func() { begins++ }, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		if strings.HasPrefix(sql, "INSERT") {
			stored = []driver.Value{args[0].Value, args[1].Value, args[2].Value}
		}
		return []string{"id", "secret", "backup"}, [][]driver.Value{stored}, nil
	}}, database.WithEncryption(ring))
	got, err := r.Insert(t.Context(), db, r.Secret.Set(encrypted.NewText("private primary")), r.Backup.Set(encrypted.NewText("private backup")))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID.IsZero() || got.Secret.Reveal() != "private primary" || begins != 1 {
		t.Fatal("encrypted insert did not generate its key and seal inside transaction")
	}
	for _, raw := range stored[1:] {
		if !strings.HasPrefix(raw.(string), "fg1:") {
			t.Fatal("plaintext was sent to SQL")
		}
	}
	statements = nil
	for _, as := range []Assignment[secureRecord]{r.Secret.Set(encrypted.NewText("change")), r.Backup.SetNull()} {
		if _, err := r.Query().Where(r.Key().Eq(got.ID)).UpdateAll(t.Context(), db, as); !errors.Is(err, fault.Invalid) {
			t.Fatal("set-based encrypted write accepted")
		}
	}
	if len(statements) != 0 || begins != 1 {
		t.Fatal("set-based rejection acquired transaction or sent SQL")
	}
	noKeys := testDB(t, testConnector{statements: &statements})
	if _, err := r.Insert(t.Context(), noKeys, r.Secret.Set(encrypted.NewText("secret"))); !errors.Is(err, fault.Missing) {
		t.Fatal("missing keyring accepted")
	}
	if len(statements) != 0 {
		t.Fatal("missing keyring sent SQL")
	}
	// The declaration's table and column are authenticated, not inferred from ciphertext.
	wrongTable := testDB(t, testConnector{columns: []string{"id", "secret", "backup"}, values: [][]driver.Value{stored}}, database.WithEncryption(ring))
	if got, err := secureRecordsAt("another_table").Query().All(t.Context(), wrongTable); err == nil || got != nil {
		t.Fatal("wrong-table hydration accepted")
	}
	wrongColumn := append([]driver.Value(nil), stored...)
	wrongColumn[1] = stored[2]
	wrongField := testDB(t, testConnector{columns: []string{"id", "secret", "backup"}, values: [][]driver.Value{wrongColumn}}, database.WithEncryption(ring))
	if got, err := r.Query().All(t.Context(), wrongField); err == nil || got != nil {
		t.Fatal("wrong-column hydration accepted")
	}
	// Each row must use its own key; a later failure must discard earlier models.
	other, err := model.NewID[secureRecord]()
	if err != nil {
		t.Fatal(err)
	}
	copied := append([]driver.Value(nil), stored...)
	copied[0] = other.String()
	bad := testDB(t, testConnector{columns: []string{"id", "secret", "backup"}, values: [][]driver.Value{stored, copied}}, database.WithEncryption(ring))
	if rows, err := r.Query().All(t.Context(), bad); err == nil || rows != nil {
		t.Fatal("wrong-key hydration published partial models")
	}
}
func TestSensitiveDiagnostics(t *testing.T) {
	r := secureRecords()
	key, err := encryption.GenerateKey("diagnostic_test")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := encryption.NewKeyring("diagnostic_test", key)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := encrypted.Binding("secure_records", "secret", int64(7))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encrypted.NewText("do-not-disclose-plaintext").Seal(t.Context(), ring, binding)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encrypted.TextCodec().Bind(sealed)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := encrypted.Binding("wrong_table", "secret", int64(7))
	if err != nil {
		t.Fatal(err)
	}
	_, failure := sealed.Open(t.Context(), ring, wrong)
	if failure == nil {
		t.Fatal("wrong binding accepted")
	}
	plain := encrypted.NewText("canary-plaintext")
	_, keyFailure := encryption.ParseKey("diagnostic_test", secret.New("canary-key-material"))
	_, ringFailure := encryption.NewKeyring("canary-key-material", key)
	rawCause := errors.New("canary-plaintext canary-key-material")
	hiddenCause := database.FoundryStatementUnknown(sqlowner.Outcomes(), "write", rawCause)
	items := []any{sealed, key, ring, binding, r.Secret.Set(sealed), failure, keyFailure, ringFailure, hiddenCause,
		r.Secret.Set(plain), r.Backup.Set(plain), r.Backup.SetNull(), r.Backup.IsNull(), r.Backup.IsNotNull(), r.Secret, r.Backup,
		&WriteError[secureRecord]{cause: failure, candidate: secureRecord{Secret: plain, Backup: value.Of(plain)}},
	}
	for _, item := range items {
		// Driver errors may retain arbitrary causes; public formatting must redact them.
		cause := fmt.Errorf("wrapped: %v", item)
		wrapped := database.FoundryStatementUnknown(sqlowner.Outcomes(), "write", cause)
		for _, item := range []any{item, wrapped, &WriteError[secureRecord]{cause: wrapped, candidate: secureRecord{Secret: plain}}} {
			for name, render := range map[string]func() string{
				"String": func() string { return fmt.Sprint(item) },
				"GoString": func() string {
					if v, ok := item.(fmt.GoStringer); ok {
						return v.GoString()
					}
					return ""
				},
				"StringMethod": func() string {
					if v, ok := item.(fmt.Stringer); ok {
						return v.String()
					}
					return ""
				},
				"%v":  func() string { return fmt.Sprintf("%v", item) },
				"%+v": func() string { return fmt.Sprintf("%+v", item) },
				"%#v": func() string { return fmt.Sprintf("%#v", item) },
				"%s":  func() string { return fmt.Sprintf("%s", item) },
				"error": func() string {
					if e, ok := item.(error); ok {
						return e.Error()
					}
					return ""
				},
				"JSON": func() string { data, _ := json.Marshal(item); return string(data) },
				"log": func() string {
					var out strings.Builder
					slog.New(slog.NewJSONHandler(&out, nil)).Info("safe", "value", item)
					return out.String()
				},
			} {
				t.Run(name, func(t *testing.T) {
					text := render()
					if strings.Contains(text, "canary-plaintext") || strings.Contains(text, "canary-key-material") || strings.Contains(text, "do-not-disclose-plaintext") || strings.Contains(text, encoded.(string)) || strings.Contains(text, key.Secret().Reveal()) {
						t.Fatal("sensitive diagnostic disclosed stored bytes")
					}
				})
			}
		}
	}
}

type afterCommitWriter struct{ db *database.DB }

func (w afterCommitWriter) Transaction(ctx context.Context, fn func(*database.Tx) error, opts ...database.TxOptions) error {
	return w.db.Transaction(ctx, func(tx *database.Tx) error {
		if err := tx.AfterCommit(func(context.Context) error { return errors.New("callback failed") }); err != nil {
			return err
		}
		return fn(tx)
	}, opts...)
}
func TestWriteOutcomeCoverage(t *testing.T) {
	r := records()
	for _, tc := range []struct {
		name                      string
		commitErr, errorAfterRows error
		callback                  bool
		outcome                   database.Outcome
		candidate                 bool
	}{
		{name: "unknown", commitErr: errors.New("commit unavailable"), outcome: database.Unknown, candidate: true},
		{name: "committed callback", callback: true, outcome: database.Committed, candidate: true},
		{name: "returning failure", errorAfterRows: errors.New("returning failed"), outcome: database.RolledBack},
	} {
		t.Run(tc.name, func(t *testing.T) {
			begins := 0
			db := testDB(t, testConnector{columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(7), "stored", nil, "12.5"}}, commitErr: tc.commitErr, closeErr: tc.errorAfterRows, onBegin: func() { begins++ }})
			var writer database.Transactor = db
			if tc.callback {
				writer = afterCommitWriter{db}
			}
			got, err := r.Update(t.Context(), writer, 7, r.Name.Set("new"))
			var outcome *database.Error
			var write *WriteError[record]
			if err == nil || got.ID != 0 || !errors.As(err, &outcome) || outcome.Outcome() != tc.outcome || errors.As(err, &write) != tc.candidate || begins != 1 {
				t.Fatalf("wrong write outcome: %v", err)
			}
			if tc.candidate && write.Candidate().ID != 7 {
				t.Fatal("missing reconciliation identity")
			}
		})
	}
	t.Run("savepoint rollback", func(t *testing.T) {
		var sql []string
		db := testDB(t, testConnector{statements: &sql, columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{}})
		err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			if _, err := r.Update(t.Context(), tx, 7, r.Name.Set("new")); err == nil {
				t.Fatal("missing RETURNING accepted")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(sql, "\n")
		if !strings.Contains(joined, "SAVEPOINT") || !strings.Contains(joined, "ROLLBACK TO SAVEPOINT") {
			t.Fatal("failed nested write did not roll back its savepoint")
		}
	})
}

func TestBatchDefaultsAndReturningOrder(t *testing.T) {
	type item struct {
		ID      int64 `foundry:"default=database"`
		Name    string
		Enabled bool `foundry:"default=database"`
	}
	type fields struct {
		Table[item, int64]
		Name    Text[item, string]
		Enabled Column[item, bool]
	}
	r := Define("batch_defaults", func(b Builder[item], m *item) fields {
		return fields{TableOf(b, &m.ID), TextOf(b, &m.Name), ColumnOf(b, &m.Enabled)}
	})
	var sql []string
	var args [][]any
	db := testDB(t, testConnector{statements: &sql, bindings: &args, columns: []string{"id", "name", "enabled"}, values: [][]driver.Value{{int64(20), "first", true}, {int64(10), "second", false}}})
	got, err := r.InsertMany(t.Context(), db, [][]Assignment[item]{{r.Name.Set("first")}, {r.Name.Set("second"), r.Enabled.Set(false)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 20 || got[1].ID != 10 || !got[0].Enabled || got[1].Enabled {
		t.Fatal("RETURNING order/default/value lost")
	}
	if len(sql) != 1 || !strings.Contains(sql[0], "DEFAULT") || len(args[0]) != 3 || args[0][2] != false {
		t.Fatal("omitted default and explicit false not preserved")
	}
}

func TestEncryptedDeclarationDefaultsRejected(t *testing.T) {
	type m struct {
		ID    int64 `foundry:"default=database"`
		Token encrypted.Text
	}
	r := Define("invalid_defaults", func(b Builder[m], v *m) Table[m, int64] { EncryptedTextOf(b, &v.Token); return TableOf(b, &v.ID) })
	if err := Check(r); !errors.Is(err, fault.Invalid) {
		t.Fatal("database-generated key accepted for encrypted field")
	}
	type n struct {
		ID    int64
		Token encrypted.Text `foundry:"default=database"`
	}
	r2 := Define("invalid_defaults", func(b Builder[n], v *n) Table[n, int64] { EncryptedTextOf(b, &v.Token); return TableOf(b, &v.ID) })
	if err := Check(r2); !errors.Is(err, fault.Invalid) {
		t.Fatal("encrypted database default accepted")
	}
}

func TestPasswordCodecBoundary(t *testing.T) {
	type account struct {
		ID     int64
		Digest password.Hash
		Backup value.Nullable[password.Hash]
	}
	type fields struct {
		Table[account, int64]
		Digest Column[account, password.Hash]
		Backup NullableColumn[account, password.Hash]
	}
	r := Define("password_records", func(b Builder[account], m *account) fields {
		return fields{TableOf(b, &m.ID), PasswordOf(b, &m.Digest), NullablePasswordOf(b, &m.Backup)}
	})
	if err := Check(r); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{columns: []string{"id", "digest", "backup"}, values: [][]driver.Value{{int64(1), "untrusted-plaintext", nil}}})
	got, err := r.Query().All(t.Context(), db)
	if err == nil || got != nil || strings.Contains(err.Error(), "untrusted-plaintext") {
		t.Fatal("password codec accepted or disclosed plaintext")
	}
	var sent []string
	writer := testDB(t, testConnector{statements: &sent})
	if _, err := r.Update(t.Context(), writer, 1, r.Digest.Set(password.Hash{})); err == nil || len(sent) != 0 {
		t.Fatal("zero hash sent SQL")
	}
	type bad struct{ Digest password.Hash }
	key := Define("bad_password_key", func(b Builder[bad], m *bad) Table[bad, password.Hash] {
		return TableOf(b, &m.Digest, Codec(password.Codec()))
	})
	if err := Check(key); !errors.Is(err, fault.Invalid) {
		t.Fatal("sensitive password identity accepted")
	}
}

func TestEncryptedBatchUsesEachGeneratedKey(t *testing.T) {
	r := secureRecords()
	ring := sensitiveKeys(t)
	begins := 0
	var envelopes []string
	db := testDB(t, testConnector{onBegin: func() { begins++ }, respondBound: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		if len(args) != 6 {
			return nil, nil, errors.New("unexpected batch bindings")
		}
		var data [][]driver.Value
		for i := 0; i < len(args); i += 3 {
			data = append(data, []driver.Value{args[i].Value, args[i+1].Value, args[i+2].Value})
			envelopes = append(envelopes, args[i+1].Value.(string))
		}
		return []string{"id", "secret", "backup"}, data, nil
	}}, database.WithEncryption(ring))
	got, err := r.InsertMany(t.Context(), db, [][]Assignment[secureRecord]{
		{r.Secret.Set(encrypted.NewText("first")), r.Backup.SetNull()},
		{r.Secret.Set(encrypted.NewText("second")), r.Backup.SetNull()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID == got[1].ID || got[0].ID.IsZero() || got[1].ID.IsZero() || got[0].Secret.Reveal() != "first" || got[1].Secret.Reveal() != "second" || begins != 1 {
		t.Fatal("batch key ownership or RETURNING changed")
	}
	item, err := encrypted.TextCodec().Decode(envelopes[0])
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := encrypted.Binding("secure_records", "secret", got[1].ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := item.Open(t.Context(), ring, wrong); err == nil {
		t.Fatal("batch envelope opened with another row's generated key")
	}
}
