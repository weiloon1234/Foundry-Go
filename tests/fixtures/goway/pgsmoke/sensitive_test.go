package pgsmoke_test

import (
	"errors"
	newmodels "foundry.test/goway/models/sensitive"
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

func TestSensitivePersistence(t *testing.T) {
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
	inserted, err := orm.Insert(ctx, db, v.Table, orm.EncryptedSet(v.Token, encrypted.NewText("private")), orm.NullableEncryptedSet(v.Backup, encrypted.NewText("backup")), orm.EncryptedSet(v.Settings, doc), orm.NullableEncryptedSet(v.Optional, doc))
	must(t, err)
	rawModels, err := orm.Raw(ctx, db, v.Table, `SELECT optional,settings,backup,token,id FROM sensitive_vaults WHERE id=$1`, inserted.ID.String())
	must(t, err)
	if len(rawModels) != 1 || rawModels[0].Token.Reveal() != "private" {
		t.Fatal("encrypted raw hydration")
	}
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
	if _, err = orm.RequireKey(ctx, db, copies, inserted.ID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("wrong-table fault: %v", err)
	}
	storedVault, err := orm.RequireKey(ctx, db, v.Table, inserted.ID)
	must(t, err)
	storedSettings, err := storedVault.Settings.Decode()
	must(t, err)
	storedOptional, ok := storedVault.Optional.Get()
	if !ok {
		t.Fatal("missing nullable encrypted JSON")
	}
	optional, err := storedOptional.Decode()
	must(t, err)
	if storedVault.Token.Reveal() != "private" || storedSettings.Region != "west" || optional.Region != "west" {
		t.Fatal("encrypted text and JSON hydration")
	}
	secondDoc, err := encrypted.NewJSON(newmodels.Settings{Region: "east"})
	must(t, err)
	second, err := orm.Insert(ctx, db, v.Table, orm.EncryptedSet(v.Token, encrypted.NewText("second private")), orm.EncryptedSet(v.Settings, secondDoc))
	must(t, err)
	secondID := second.ID
	current, err := orm.RequireKey(ctx, db, v.Table, secondID)
	must(t, err)
	if current.Token.Reveal() != "second private" || !current.Backup.IsNull() || !current.Optional.IsNull() {
		t.Fatal("omitted nullable encrypted fields")
	}
	_, err = orm.Update(ctx, db, v.Table.Query(), v.Table.Key(), inserted.ID, orm.EncryptedSet(v.Token, encrypted.NewText("changed first")), orm.NullableEncryptedSetNull(v.Backup), orm.NullableEncryptedSetNull(v.Optional))
	must(t, err)
	current, err = orm.RequireKey(ctx, db, v.Table, inserted.ID)
	must(t, err)
	if current.Token.Reveal() != "changed first" || !current.Backup.IsNull() || !current.Optional.IsNull() {
		t.Fatal("encrypted update and explicit null hydration")
	}
	_, err = orm.Update(ctx, db, v.Table.Query(), v.Table.Key(), secondID, orm.EncryptedSet(v.Token, encrypted.NewText("changed second")), orm.NullableEncryptedSet(v.Backup, encrypted.NewText("new backup")), orm.NullableEncryptedSet(v.Optional, doc))
	must(t, err)
	second, err = orm.RequireKey(ctx, db, v.Table, second.ID)
	must(t, err)
	backup, ok := second.Backup.Get()
	if !ok || backup.Reveal() != "new backup" || second.Token.Reveal() != "changed second" {
		t.Fatal("encrypted update hydration")
	}
	_, err = orm.Update(ctx, db, v.Table.Query(), v.Table.Key(), inserted.ID, orm.NullableEncryptedSet(v.Backup, encrypted.NewText("clear me")), orm.NullableEncryptedSet(v.Optional, doc))
	must(t, err)
	_, err = orm.Update(ctx, db, v.Table.Query(), v.Table.Key(), inserted.ID, orm.NullableEncryptedSetNull(v.Backup), orm.NullableEncryptedSetNull(v.Optional))
	must(t, err)
	current, err = orm.RequireKey(ctx, db, v.Table, inserted.ID)
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
	// Copy one existing envelope to another row. Hydration must reject it.
	_, err = db.Exec(ctx, `UPDATE sensitive_vaults SET token=(SELECT token FROM sensitive_vaults WHERE id=$1) WHERE id=$2`, inserted.ID.String(), secondID.String())
	must(t, err)
	if _, err = orm.RequireKey(ctx, db, v.Table, secondID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("wrong-row fault: %v", err)
	}
	// A same-row different-column envelope is equally invalid.
	_, err = db.Exec(ctx, `UPDATE sensitive_vaults SET token=backup WHERE id=$1`, secondID.String())
	must(t, err)
	if _, err = orm.RequireKey(ctx, db, v.Table, secondID); !errors.Is(err, fault.Invalid) {
		t.Fatalf("wrong-column fault: %v", err)
	}
	hasher, err := password.New(password.DefaultConfig())
	must(t, err)
	plain, err := password.NewPlaintext(secret.New("fixture password"))
	must(t, err)
	hash, err := hasher.Hash(ctx, plain)
	must(t, err)
	a := newmodels.Accounts
	account, err := orm.Insert(ctx, db, a.Table, orm.ColumnSet(a.Digest, hash))
	must(t, err)
	if !account.Backup.IsNull() {
		t.Fatal("nullable hash did not retain SQL NULL")
	}
	storedAccount, err := orm.RequireKey(ctx, db, a.Table, account.ID)
	must(t, err)
	match, err := hasher.Check(ctx, plain, storedAccount.Digest)
	must(t, err)
	if !match {
		t.Fatal("stored password round trip")
	}
	_, err = orm.Update(ctx, db, a.Table.Query(), a.Table.Key(), account.ID, orm.NullableColumnSet(a.Backup, hash))
	must(t, err)
	account, err = orm.Require(ctx, db, a.Query().Where(orm.KeyEq(a.Key(), account.ID), orm.ColumnEq(a.Digest, hash)).OrderBy(orm.ColumnAsc(a.Digest)))
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
