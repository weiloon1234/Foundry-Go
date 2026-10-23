package pgsmoke_test

import (
	"errors"
	newmodels "foundry.test/goway/models/sensitive"
	oldmodels "foundry.test/goway/oldmodels/sensitive"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"strings"
	"testing"
)

func TestSensitivePersistenceAcrossAPIs(t *testing.T) {
	scope := postgres.Isolate(t)
	key, err := encryption.GenerateKey("field_test")
	must(t, err)
	keys, err := encryption.NewKeyring("field_test", key)
	must(t, err)
	db := scope.Open(t, database.WithEncryption(keys))
	ctx := t.Context()
	_, err = db.Exec(ctx, `CREATE TABLE sensitive_vaults (id uuid PRIMARY KEY,token text NOT NULL,backup text,settings text NOT NULL,optional text)`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE TABLE sensitive_accounts (id uuid PRIMARY KEY,digest text NOT NULL,backup text)`)
	must(t, err)
	v := newmodels.Vaults
	doc, err := encrypted.NewJSON(newmodels.Settings{Region: "west"})
	must(t, err)
	inserted, err := v.Insert(ctx, db, v.Token.Set(encrypted.NewText("private")), v.Backup.Set(encrypted.NewText("backup")), v.Settings.Set(doc), v.Optional.Set(doc))
	must(t, err)
	storedRows, err := db.Query(ctx, `SELECT token,backup,settings,optional FROM sensitive_vaults WHERE id=$1`, inserted.ID.String())
	must(t, err)
	if !storedRows.Next() {
		t.Fatal("missing stored encrypted row")
	}
	var token, backupEnvelope, settingsEnvelope, optionalEnvelope string
	must(t, storedRows.Scan(&token, &backupEnvelope, &settingsEnvelope, &optionalEnvelope))
	must(t, storedRows.Close())
	for _, envelope := range []string{token, backupEnvelope, settingsEnvelope, optionalEnvelope} {
		if !strings.HasPrefix(envelope, "fg1:") || strings.Contains(envelope, "private") || strings.Contains(envelope, "backup") || strings.Contains(envelope, "west") {
			t.Fatal("stored encryption envelope exposed plaintext")
		}
	}
	// The same ID and column names in another table still authenticate differently.
	_, err = db.Exec(ctx, `CREATE TABLE sensitive_vault_copies (LIKE sensitive_vaults INCLUDING ALL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO sensitive_vault_copies SELECT * FROM sensitive_vaults WHERE id=$1`, inserted.ID.String())
	must(t, err)
	copies := orm.Define("sensitive_vault_copies", func(b orm.Builder[newmodels.Vault], m *newmodels.Vault) orm.Table[newmodels.Vault, model.ID[newmodels.Vault]] {
		orm.EncryptedTextOf(b, &m.Token)
		orm.NullableEncryptedTextOf(b, &m.Backup)
		orm.EncryptedJSONOf(b, &m.Settings)
		orm.NullableEncryptedJSONOf(b, &m.Optional)
		return orm.TableOf(b, &m.ID)
	})
	if _, err = copies.Require(ctx, db, inserted.ID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("wrong-table fault: %v", err)
	}
	oldID, err := model.ParseID[oldmodels.Vault](inserted.ID.String())
	must(t, err)
	old, err := oldmodels.QuerySensitiveVaults().RequireFind(ctx, db, oldID)
	must(t, err)
	oldSettings, err := old.Settings.Decode()
	must(t, err)
	oldOptional, ok := old.Optional.Get()
	if !ok {
		t.Fatal("missing nullable encrypted JSON")
	}
	optional, err := oldOptional.Decode()
	must(t, err)
	if old.Token.Reveal() != "private" || oldSettings.Region != "west" || optional.Region != "west" {
		t.Fatal("new-to-old encrypted hydration")
	}
	oldDoc, err := encrypted.NewJSON(oldmodels.Settings{Region: "east"})
	must(t, err)
	second, err := oldmodels.QuerySensitiveVaults().Create(ctx, db, oldmodels.VaultDraft{}.SetToken(encrypted.NewText("old private")).SetSettings(oldDoc))
	must(t, err)
	newID, err := model.ParseID[newmodels.Vault](second.ID.String())
	must(t, err)
	current, err := v.Require(ctx, db, newID)
	must(t, err)
	if current.Token.Reveal() != "old private" || !current.Backup.IsNull() || !current.Optional.IsNull() {
		t.Fatal("old-to-new encrypted hydration")
	}
	_, err = oldmodels.QuerySensitiveVaults().Update(ctx, db, oldID, oldmodels.VaultDraft{}.SetToken(encrypted.NewText("changed by old")).ClearBackup().ClearOptional())
	must(t, err)
	current, err = v.Require(ctx, db, inserted.ID)
	must(t, err)
	if current.Token.Reveal() != "changed by old" || !current.Backup.IsNull() || !current.Optional.IsNull() {
		t.Fatal("old update/new read")
	}
	_, err = v.Update(ctx, db, newID, v.Token.Set(encrypted.NewText("changed by new")), v.Backup.Set(encrypted.NewText("new backup")), v.Optional.Set(doc))
	must(t, err)
	second, err = oldmodels.QuerySensitiveVaults().RequireFind(ctx, db, second.ID)
	must(t, err)
	backup, ok := second.Backup.Get()
	if !ok || backup.Reveal() != "new backup" || second.Token.Reveal() != "changed by new" {
		t.Fatal("new update/old read")
	}
	_, err = v.Update(ctx, db, inserted.ID, v.Backup.Set(encrypted.NewText("clear me")), v.Optional.Set(doc))
	must(t, err)
	_, err = v.Update(ctx, db, inserted.ID, v.Backup.SetNull(), v.Optional.SetNull())
	must(t, err)
	current, err = v.Require(ctx, db, inserted.ID)
	must(t, err)
	if !current.Backup.IsNull() || !current.Optional.IsNull() {
		t.Fatal("new SetNull did not persist")
	}
	nullRows, err := db.Query(ctx, `SELECT backup IS NULL,optional IS NULL FROM sensitive_vaults WHERE id=$1`, inserted.ID.String())
	must(t, err)
	if !nullRows.Next() {
		t.Fatal("missing SetNull row")
	}
	var backupNull, optionalNull bool
	must(t, nullRows.Scan(&backupNull, &optionalNull))
	must(t, nullRows.Close())
	if !backupNull || !optionalNull {
		t.Fatal("SetNull did not store SQL NULL")
	}
	// Copy one existing envelope to another row. Both APIs must refuse to publish it.
	_, err = db.Exec(ctx, `UPDATE sensitive_vaults SET token=(SELECT token FROM sensitive_vaults WHERE id=$1) WHERE id=$2`, inserted.ID.String(), newID.String())
	must(t, err)
	if _, err = v.Require(ctx, db, newID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("new API wrong-row fault: %v", err)
	}
	if _, err = oldmodels.QuerySensitiveVaults().RequireFind(ctx, db, second.ID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("old API wrong-row fault: %v", err)
	}
	// A same-row different-column envelope is equally invalid.
	_, err = db.Exec(ctx, `UPDATE sensitive_vaults SET token=backup WHERE id=$1`, newID.String())
	must(t, err)
	if _, err = v.Require(ctx, db, newID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("new API wrong-column fault: %v", err)
	}
	if _, err = oldmodels.QuerySensitiveVaults().RequireFind(ctx, db, second.ID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("old API wrong-column fault: %v", err)
	}
	hasher, err := password.New(password.DefaultConfig())
	must(t, err)
	plain, err := password.NewPlaintext(secret.New("fixture password"))
	must(t, err)
	hash, err := hasher.Hash(ctx, plain)
	must(t, err)
	a := newmodels.Accounts
	account, err := a.Insert(ctx, db, a.Digest.Set(hash))
	must(t, err)
	if !account.Backup.IsNull() {
		t.Fatal("nullable hash did not retain SQL NULL")
	}
	oldAccountID, err := model.ParseID[oldmodels.Account](account.ID.String())
	must(t, err)
	oldAccount, err := oldmodels.QuerySensitiveAccounts().RequireFind(ctx, db, oldAccountID)
	must(t, err)
	match, err := hasher.Check(ctx, plain, oldAccount.Digest)
	must(t, err)
	if !match {
		t.Fatal("stored password round trip")
	}
	_, err = oldmodels.QuerySensitiveAccounts().Update(ctx, db, oldAccountID, oldmodels.AccountDraft{}.SetBackup(hash))
	must(t, err)
	account, err = a.Query().Where(a.Key().Eq(account.ID), a.Digest.Eq(hash)).OrderBy(a.Digest.Asc()).Require(ctx, db)
	must(t, err)
	stored, ok := account.Backup.Get()
	if !ok {
		t.Fatal("missing nullable hash")
	}
	match, err = hasher.Check(ctx, plain, stored)
	must(t, err)
	if !match {
		t.Fatal("nullable hash round trip")
	}
}
