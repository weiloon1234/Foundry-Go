package goway_test

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
			_, err = v.Insert(t.Context(), db, v.Key().Set(newID), v.Token.Set(encrypted.NewText("first")), v.Backup.Set(encrypted.NewText("backup")), v.Settings.Set(newJSON), v.Optional.Set(newJSON))
		}
		if err != nil {
			t.Fatal(err)
		}
		// The native reader opens the exact stored envelopes.
		{
			got, e := newmodels.Vaults.Require(t.Context(), db, newID)
			if e != nil || got.Token.Reveal() != "first" {
				t.Fatal("decryption", e)
			}
		}
		{
			v := newmodels.Vaults
			_, err = v.Update(t.Context(), db, newID, v.Token.Set(encrypted.NewText("second")), v.Backup.SetNull(), v.Optional.SetNull())
		}
		if err != nil {
			t.Fatal(err)
		}

		if _, err := newmodels.Vaults.Require(t.Context(), db, newID); err != nil {
			t.Fatal(err)
		}
		newGot, err := newmodels.Vaults.Require(t.Context(), db, newID)
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
			_, err = a.Insert(t.Context(), db, a.Key().Set(newID), a.Digest.Set(hash), a.Backup.Set(hash))
		}
		if err != nil {
			t.Fatal(err)
		}

		a := newmodels.Accounts
		if _, err := a.Query().Where(a.Digest.Eq(hash)).OrderBy(a.Digest.Asc()).Where(a.Key().Eq(newID)).Require(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		current, err := a.Query().Where(a.Digest.Eq(hash), a.Key().Eq(newID)).OrderBy(a.Digest.Asc()).Require(t.Context(), db)
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
	inserted, err := v.Insert(t.Context(), db, v.Token.Set(encrypted.NewText("private")), v.Settings.Set(doc))
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
				err = v.Query().Each(t.Context(), db, func(m newmodels.Vault) error {
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
