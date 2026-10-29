package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "time"

import (
	"context"
	"database/sql/driver"
	"errors"
	newmodels "foundry.test/goway/models/sensitive"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func fieldTestKeys(t *testing.T) *encryption.Keyring {
	t.Helper()
	key, err := encryption.GenerateKey("field_test")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring("field_test", key)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
func sensitiveDB(t *testing.T, store *sensitiveStore, keys *encryption.Keyring) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: sensitiveConnector{store}}, database.DefaultPoolConfig(), database.WithEncryption(keys))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}
func TestSensitiveOldAndNewAgree(t *testing.T) {
	keys := fieldTestKeys(t)

	newID, err := model.ParseID[newmodels.Vault](orderID)
	if err != nil {
		t.Fatal(err)
	}
	var transcripts []string
	for range 2 {
		store := &sensitiveStore{}
		db := sensitiveDB(t, store, keys)

		newJSON, err := encrypted.NewJSON(newmodels.Settings{Region: "west"})
		if err != nil {
			t.Fatal(err)
		}
		{
			v := newmodels.Vaults
			_, err = orm.Insert(t.Context(), db, v.Table, orm.KeySet(v.Key(), newID), orm.EncryptedSet(v.Token, encrypted.NewText("first")), orm.NullableEncryptedSet(v.Backup, encrypted.NewText("backup")), orm.EncryptedSet(v.Settings, newJSON), orm.NullableEncryptedSet(v.Optional, newJSON))
		}
		if err != nil {
			t.Fatal(err)
		}
		// The native reader opens the exact stored envelopes.
		{
			got, e := orm.RequireKey(t.Context(), db, newmodels.Vaults.Table, newID)
			if e != nil || got.Token.Reveal() != "first" {
				t.Fatal("decryption", e)
			}
		}
		{
			v := newmodels.Vaults
			_, err = orm.Update(t.Context(), db, v.Table.Query(), v.Table.Key(), newID, orm.EncryptedSet(v.Token, encrypted.NewText("second")), orm.NullableEncryptedSetNull(v.Backup), orm.NullableEncryptedSetNull(v.Optional))
		}
		if err != nil {
			t.Fatal(err)
		}

		if _, err := orm.RequireKey(t.Context(), db, newmodels.Vaults.Table, newID); err != nil {
			t.Fatal(err)
		}
		newGot, err := orm.RequireKey(t.Context(), db, newmodels.Vaults.Table, newID)
		if err != nil {
			t.Fatal(err)
		}

		b, e := newGot.Settings.Decode()
		if e != nil {
			t.Fatal(e)
		}
		if !oracleEqual(t, "sensitive_golden/decrypted", oracleSnapshot([]any{newGot.Token.Reveal(), b.Region, newGot.Backup.IsNull(), newGot.Optional.IsNull()})) {
			t.Fatal("decrypted parity")
		}
		if newGot.Token.Reveal() != "second" || !newGot.Backup.IsNull() || !newGot.Optional.IsNull() {
			t.Fatal("decrypted values differ")
		}
		transcripts = append(transcripts, store.transcript.String())
	}
	if !oracleEqual(t, "sensitive_golden/2", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func TestPasswordOldAndNewAgree(t *testing.T) {
	hasher, err := password.New(password.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	plain, err := password.NewPlaintext(secret.New("fixture-password"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}

	newID, err := model.ParseID[newmodels.Account](orderID)
	if err != nil {
		t.Fatal(err)
	}
	var transcripts []string
	for range 2 {
		store := &sensitiveStore{}
		db := sensitiveDB(t, store, fieldTestKeys(t))
		{
			a := newmodels.Accounts
			_, err = orm.Insert(t.Context(), db, a.Table, orm.KeySet(a.Key(), newID), orm.ColumnSet(a.Digest, hash), orm.NullableColumnSet(a.Backup, hash))
		}
		if err != nil {
			t.Fatal(err)
		}

		a := newmodels.Accounts
		if _, err := orm.Require(t.Context(), db, a.Query().Where(orm.ColumnEq(a.Digest, hash)).OrderBy(orm.ColumnAsc(a.Digest)).Where(orm.KeyEq(a.Key(), newID))); err != nil {
			t.Fatal(err)
		}
		current, err := orm.Require(t.Context(), db, a.Query().Where(orm.ColumnEq(a.Digest, hash), orm.KeyEq(a.Key(), newID)).OrderBy(orm.ColumnAsc(a.Digest)))
		if err != nil {
			t.Fatal(err)
		}
		if !oracleEqual(t, "sensitive_golden/password", oracleSnapshot([]bool{current.Digest == hash, current.Backup == value.Of(hash)})) {
			t.Fatal("frozen legacy comparison differs")
		}

		transcripts = append(transcripts, store.transcript.String())
	}
	if !oracleEqual(t, "sensitive_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}

// Stream complete earlier rows, never the failed row or its successors.
func TestSensitiveStreamingFailureParity(t *testing.T) {
	keys := fieldTestKeys(t)
	store := &sensitiveStore{}
	db := sensitiveDB(t, store, keys)
	doc, err := encrypted.NewJSON(newmodels.Settings{Region: "west"})
	if err != nil {
		t.Fatal(err)
	}
	v := newmodels.Vaults
	inserted, err := orm.Insert(t.Context(), db, v.Table, orm.EncryptedSet(v.Token, encrypted.NewText("private")), orm.EncryptedSet(v.Settings, doc))
	if err != nil {
		t.Fatal(err)
	}
	good := store.rows["sensitive_vaults"][inserted.ID.String()]
	other, err := model.NewIDAt[newmodels.Vault](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]driver.Value(nil), good...)
	bad[0] = other.String()
	store.readBatch = [][]driver.Value{good, bad, good}
	observations := []string{}
	for _, style := range []string{"new"} {
		t.Run(style, func(t *testing.T) {
			seen := 0
			values := []any{}
			var err error
			{
				err = orm.Each(t.Context(), db, v.Query(), func(m newmodels.Vault) error {
					if m.ID != inserted.ID || m.Token.Reveal() != "private" {
						t.Fatal("partial or incorrect row yielded")
					}
					values = append(values, []any{m.ID.String() == inserted.ID.String(), m.Token.Reveal()})
					seen++
					return nil
				})
			}
			observations = append(observations, oracleSnapshot([]any{values, seen, errors.Is(err, fault.Invalid)}))
			if !errors.Is(err, fault.Invalid) || seen != 1 {
				t.Fatalf("stream yielded %d; fault %v", seen, err)
			}
		})
	}
	if !oracleEqual(t, "sensitive_golden/stream_failure", observations[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}
