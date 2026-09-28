package passwords_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func passwordModels(t *testing.T, work func(*database.Tx) error) {
	t.Helper()
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`SET LOCAL search_path TO "` + schema + `"`, `CREATE TABLE password_accounts(id uuid PRIMARY KEY,email text NOT NULL UNIQUE,digest text NOT NULL,backup text,enabled boolean NOT NULL)`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		return work(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTypedPasswordModelPersistsHydratesAndComparesWithoutDisclosure(t *testing.T) {
	passwordModels(t, func(tx *database.Tx) error {
		ctx := t.Context()
		hasher, err := password.New(password.DefaultConfig())
		if err != nil {
			return err
		}
		plain, err := password.NewPlaintext(secret.New("exact password"))
		if err != nil {
			return err
		}
		account, err := passwords.CreateAccount(ctx, tx, hasher, passwords.LoginRequest{Email: "member@example.test", Password: plain})
		if err != nil {
			return err
		}
		q, f := passwords.QueryPasswordAccounts(), passwords.AccountFields()
		loaded, err := q.RequireFind(ctx, tx, account.ID)
		if err != nil {
			return err
		}
		if loaded.Digest != account.Digest || !loaded.Backup.IsNull() {
			return errors.New("stored hash or nullable field changed")
		}
		if ok, err := hasher.Check(ctx, plain, loaded.Digest); err != nil || !ok {
			return errors.New("hydrated hash did not verify")
		}
		encoded, err := json.Marshal(loaded)
		if err != nil {
			return err
		}
		if strings.Contains(string(encoded), "$argon2id$") || strings.Contains(string(encoded), "exact password") {
			return errors.New("model JSON disclosed password data")
		}
		projection, err := passwords.ProjectCredentialProjection(q).SelectDigest(f.Digest.Value()).Query().RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		if projection.Digest != account.Digest {
			return errors.New("projection lost hash type or stored bytes")
		}
		next, err := hasher.Hash(ctx, plain)
		if err != nil {
			return err
		}
		updated, err := passwords.ReplaceHash(ctx, tx, account, next)
		if err != nil {
			return err
		}
		if updated.Digest != next {
			return errors.New("typed replacement was not persisted")
		}
		if _, err := passwords.ReplaceHash(ctx, tx, account, account.Digest); !errors.Is(err, database.NotFound) {
			return errors.New("stale password replacement overwrote new hash")
		}
		updated, err = q.Update(ctx, tx, account.ID, passwords.AccountDraft{}.SetBackup(next))
		if err != nil {
			return err
		}
		backup, ok := updated.Backup.Get()
		if !ok || backup != next {
			return errors.New("nullable password hydration changed value")
		}
		updated, err = q.Update(ctx, tx, account.ID, passwords.AccountDraft{}.ClearBackup())
		if err != nil {
			return err
		}
		if !updated.Backup.IsNull() {
			return errors.New("explicit hash NULL lost")
		}
		changes, err := passwords.CompareAccount(value.Set(account), value.Set(updated), passwords.AccountDraft{}.SetDigest(next))
		if err != nil {
			return err
		}
		// The shared audit boundary must redact by codec, although the name is Digest.
		captured, err := record.CaptureField[passwords.Account]("digest", password.Codec(), changes.Fields().Digest, record.Automatic)
		if err != nil {
			return err
		}
		builder, err := record.NewBuilder(account.FoundryReference(), lifecycle.Update, "id", record.Automatic)
		if err != nil {
			return err
		}
		id, err := record.CaptureField[passwords.Account]("id", codec.ID[passwords.Account](), changes.Fields().ID, record.Automatic)
		if err != nil {
			return err
		}
		if err := builder.Add(id); err != nil {
			return err
		}
		if err := builder.Add(captured); err != nil {
			return err
		}
		history, err := builder.Build()
		if err != nil {
			return err
		}
		view, err := record.Inspect(history)
		if err != nil {
			return err
		}
		digest, err := record.ReadField(view, "digest", password.Codec())
		if err != nil {
			return err
		}
		field, present := digest.Get()
		if !present || field.After().State() != record.Redacted || !field.Changed() {
			return errors.New("typed audit omitted its redacted change")
		}
		payload, err := history.Entry().Payload()
		if err != nil {
			return err
		}
		if strings.Contains(payload, "$argon2id$") {
			return errors.New("audit capture disclosed hash")
		}
		if _, err := q.OrderBy(f.Digest.Asc()).CursorPaginate(ctx, tx, query.CursorRequest[passwords.Account]{Size: 1}); !errors.Is(err, fault.Invalid) {
			return errors.New("model cursor accepted sensitive sort key")
		}
		result := query.CursorFor(passwords.ProjectCredentialProjection(q).SelectDigest(f.Digest.Value()).Query())
		rf := passwords.CredentialProjectionFieldsAt(result.Scope())
		if _, err := result.OrderBy(rf.Digest.Asc()).UniqueBy(rf.Digest.Group()).Compile(); !errors.Is(err, fault.Invalid) {
			return errors.New("projection cursor accepted sensitive key")
		}
		if _, err := q.Update(ctx, tx, account.ID, passwords.AccountDraft{}.SetDigest(password.Hash{})); !errors.Is(err, fault.Invalid) {
			return errors.New("zero hash accepted")
		}
		preserved, err := q.RequireFind(ctx, tx, account.ID)
		if err != nil {
			return err
		}
		if preserved.Digest != next {
			return errors.New("failed mutation changed hash")
		}
		return nil
	})
}

// Keep the stored-field selection visible to the real consumer LSP probe.
func storedDigest(account passwords.Account) password.Hash { return account.Digest }
