package orm_test

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type passwordMutatorRow struct {
	ID     int64
	Digest password.Hash
	Backup value.Nullable[password.Hash]
}

func (passwordMutatorRow) MutateDigest(password.Hash) (password.Hash, error) {
	return replacementHash()
}
func (passwordMutatorRow) MutateBackup(password.Hash) (password.Hash, error) {
	return replacementHash()
}
func replacementHash() (password.Hash, error) {
	return password.ParseHash(secret.New("$2b$04$" + strings.Repeat("b", 53)))
}

type passwordMutatorTable struct {
	orm.Table[passwordMutatorRow, int64]
	Digest orm.InputColumn[passwordMutatorRow, password.Hash, password.Hash]
	Backup orm.NullableInputColumn[passwordMutatorRow, password.Hash, password.Hash]
}

func TestPasswordConstructorsBindRequiredAndNullableMutators(t *testing.T) {
	fields := orm.Define("password_mutators", func(b orm.Builder[passwordMutatorRow], m *passwordMutatorRow) passwordMutatorTable {
		return passwordMutatorTable{orm.TableOf(b, &m.ID), orm.InputColumnOf[passwordMutatorRow, password.Hash](b, &m.Digest, nil, orm.Codec(password.Codec())), orm.NullableInputColumnOf[passwordMutatorRow, password.Hash](b, &m.Backup, nil, orm.Codec(password.Codec()))}
	})
	if err := orm.Check(fields); err != nil {
		t.Fatal(err)
	}
	input, err := password.ParseHash(secret.New("$2b$04$" + strings.Repeat("a", 53)))
	if err != nil {
		t.Fatal(err)
	}
	output, err := replacementHash()
	if err != nil {
		t.Fatal(err)
	}
	encoded := output.Encoded().Reveal()
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		var backup driver.Value = encoded
		if strings.HasPrefix(sql, "UPDATE") {
			backup = nil
		}
		return []string{"id", "digest", "backup"}, [][]driver.Value{{int64(1), encoded, backup}}, nil
	}})
	got, err := orm.Insert(t.Context(), db, fields.Table, orm.KeySet(fields.Key(), 1), orm.InputColumnSet(fields.Digest, input), orm.NullableInputColumnSet(fields.Backup, input))
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest.Encoded().Reveal() != encoded || bindings[0][1] != encoded || bindings[0][2] != encoded {
		t.Fatal("password mutators did not transform stored values")
	}
	got, err = orm.Update(t.Context(), db, fields.Table.Query(), fields.Table.Key(), 1, orm.NullableInputColumnSetNull(fields.Backup))
	if err != nil || !got.Backup.IsNull() || bindings[1][0] != nil {
		t.Fatal("nullable hash mutator changed explicit NULL", err)
	}
}

func TestPasswordMutatorDeclarationNamesInputCarrier(t *testing.T) {
	fields := orm.Define("password_roles", func(b orm.Builder[passwordMutatorRow], m *passwordMutatorRow) orm.Table[passwordMutatorRow, int64] {
		orm.PasswordOf(b, &m.Digest)
		orm.NullablePasswordOf(b, &m.Backup)
		return orm.TableOf(b, &m.ID)
	})
	err := orm.Check(fields)
	if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "Digest") || !strings.Contains(err.Error(), "orm.InputColumnOf") || !strings.Contains(err.Error(), "Backup") || !strings.Contains(err.Error(), "orm.NullableInputColumnOf") {
		t.Fatal(err)
	}
}
