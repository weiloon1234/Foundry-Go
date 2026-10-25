package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	newmodels "foundry.test/goway/models/sensitive"
	oldmodels "foundry.test/goway/oldmodels/sensitive"
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
	oldID, err := model.ParseID[oldmodels.Vault](orderID)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := model.ParseID[newmodels.Vault](orderID)
	if err != nil {
		t.Fatal(err)
	}
	var transcripts []string
	for _, oldFirst := range []bool{true, false} {
		store := &sensitiveStore{}
		db := sensitiveDB(t, store, keys)
		oldJSON, err := encrypted.NewJSON(oldmodels.Settings{Region: "west"})
		if err != nil {
			t.Fatal(err)
		}
		newJSON, err := encrypted.NewJSON(newmodels.Settings{Region: "west"})
		if err != nil {
			t.Fatal(err)
		}
		if oldFirst {
			_, err = oldmodels.QuerySensitiveVaults().Create(t.Context(), db, oldmodels.VaultDraft{}.SetID(oldID).SetToken(encrypted.NewText("first")).SetBackup(encrypted.NewText("backup")).SetSettings(oldJSON).SetOptional(oldJSON))
		} else {
			v := newmodels.Vaults
			_, err = v.Insert(t.Context(), db, v.Key().Set(newID), v.Token.Set(encrypted.NewText("first")), v.Backup.Set(encrypted.NewText("backup")), v.Settings.Set(newJSON), v.Optional.Set(newJSON))
		}
		if err != nil {
			t.Fatal(err)
		}
		// The opposite API must open the exact stored envelopes.
		if oldFirst {
			got, e := newmodels.Vaults.Require(t.Context(), db, newID)
			if e != nil || got.Token.Reveal() != "first" {
				t.Fatal("old-to-new decryption", e)
			}
		} else {
			got, e := oldmodels.QuerySensitiveVaults().RequireFind(t.Context(), db, oldID)
			if e != nil || got.Token.Reveal() != "first" {
				t.Fatal("new-to-old decryption", e)
			}
		}
		if oldFirst {
			v := newmodels.Vaults
			_, err = v.Update(t.Context(), db, newID, v.Token.Set(encrypted.NewText("second")), v.Backup.SetNull(), v.Optional.SetNull())
		} else {
			_, err = oldmodels.QuerySensitiveVaults().Update(t.Context(), db, oldID, oldmodels.VaultDraft{}.SetToken(encrypted.NewText("second")).ClearBackup().ClearOptional())
		}
		if err != nil {
			t.Fatal(err)
		}
		oldGot, err := oldmodels.QuerySensitiveVaults().RequireFind(t.Context(), db, oldID)
		if err != nil {
			t.Fatal(err)
		}
		newGot, err := newmodels.Vaults.Require(t.Context(), db, newID)
		if err != nil {
			t.Fatal(err)
		}
		a, e := oldGot.Settings.Decode()
		if e != nil {
			t.Fatal(e)
		}
		b, e := newGot.Settings.Decode()
		if e != nil {
			t.Fatal(e)
		}
		if !oracleEqual(t, "sensitive_golden/decrypted", oracleSnapshot([]any{oldGot.Token.Reveal(), a.Region, oldGot.Backup.IsNull(), oldGot.Optional.IsNull()}), oracleSnapshot([]any{newGot.Token.Reveal(), b.Region, newGot.Backup.IsNull(), newGot.Optional.IsNull()})) {
			t.Fatal("decrypted parity")
		}

		if oldGot.Token.Reveal() != "second" || newGot.Token.Reveal() != "second" || a.Region != b.Region || !oldGot.Backup.IsNull() || !newGot.Backup.IsNull() || !oldGot.Optional.IsNull() || !newGot.Optional.IsNull() {
			t.Fatal("decrypted values differ")
		}
		transcripts = append(transcripts, store.transcript.String())
	}
	if !oracleEqual(t, "sensitive_golden/2", transcripts[0], transcripts[1]) {
		t.Fatalf("sensitive SQL or binding shapes differ:\n%s", goldenDiff(transcripts[0], transcripts[1]))
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
	oldID, err := model.ParseID[oldmodels.Account](orderID)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := model.ParseID[newmodels.Account](orderID)
	if err != nil {
		t.Fatal(err)
	}
	var transcripts []string
	for _, oldFirst := range []bool{true, false} {
		store := &sensitiveStore{}
		db := sensitiveDB(t, store, fieldTestKeys(t))
		if oldFirst {
			_, err = oldmodels.QuerySensitiveAccounts().Create(t.Context(), db, oldmodels.AccountDraft{}.SetID(oldID).SetDigest(hash).SetBackup(hash))
		} else {
			a := newmodels.Accounts
			_, err = a.Insert(t.Context(), db, a.Key().Set(newID), a.Digest.Set(hash), a.Backup.Set(hash))
		}
		if err != nil {
			t.Fatal(err)
		}
		f := oldmodels.AccountFields()
		old, err := oldmodels.QuerySensitiveAccounts().Where(f.Digest.Eq(hash)).OrderBy(f.Digest.Asc()).RequireFind(t.Context(), db, oldID)
		if err != nil {
			t.Fatal(err)
		}
		a := newmodels.Accounts
		current, err := a.Query().Where(a.Digest.Eq(hash), a.Key().Eq(newID)).OrderBy(a.Digest.Asc()).Require(t.Context(), db)
		if err != nil {
			t.Fatal(err)
		}
		if !oracleEqual(t, "sensitive_golden/password", oracleSnapshot([]bool{old.Digest == hash, old.Backup == value.Of(hash)}), oracleSnapshot([]bool{current.Digest == hash, current.Backup == value.Of(hash)})) {
			t.Fatal("password parity")
		}

		if old.Digest != current.Digest || old.Digest != hash || old.Backup != current.Backup {
			t.Fatal("password hydrated values differ")
		}
		transcripts = append(transcripts, store.transcript.String())
	}
	if !oracleEqual(t, "sensitive_golden/1", transcripts[0], transcripts[1]) {
		t.Fatalf("password SQL/bindings differ:\n%s", goldenDiff(transcripts[0], transcripts[1]))
	}
}

// Both APIs stream complete earlier rows, but never the failed row or its successors.
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
	other, err := model.NewID[newmodels.Vault]()
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]driver.Value(nil), good...)
	bad[0] = other.String()
	store.readBatch = [][]driver.Value{good, bad, good}
	observations := []string{}
	for _, style := range []string{"old", "new"} {
		t.Run(style, func(t *testing.T) {
			seen := 0
			values := []any{}
			var err error
			if style == "old" {
				err = oldmodels.QuerySensitiveVaults().Each(t.Context(), db, func(m oldmodels.Vault) error {
					if m.ID.String() != inserted.ID.String() || m.Token.Reveal() != "private" {
						t.Fatal("partial or incorrect row yielded")
					}
					values = append(values, []any{m.ID.String() == inserted.ID.String(), m.Token.Reveal()})
					seen++
					return nil
				})
			} else {
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
	if !oracleEqual(t, "sensitive_golden/stream_failure", observations[0], observations[1]) {
		t.Fatal("stream failure parity")
	}
}
