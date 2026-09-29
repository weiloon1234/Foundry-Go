package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/mfa/command"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestMFAReencryptParsingIsBoundedAndPure(t *testing.T) {
	var help bytes.Buffer
	if _, err := command.Parse([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) || !strings.Contains(help.String(), "mfa reencrypt") {
		t.Fatal("help did not describe the command", err)
	}
	for _, args := range [][]string{{"mfa", "reencrypt"}, {"mfa", "reencrypt", "--batch", "10", "--max-batches", "3", "--format", "json"}} {
		if _, err := command.Parse(args, &help); err != nil {
			t.Fatal("valid invocation rejected", args, err)
		}
	}
	for _, args := range [][]string{{"mfa"}, {"mfa", "rotate"}, {"mfa", "reencrypt", "--batch", "0"}, {"mfa", "reencrypt", "--batch", "100000"}, {"mfa", "reencrypt", "--max-batches", "0"}, {"mfa", "reencrypt", "extra"}, {"mfa", "reencrypt", "--format", "xml"}} {
		if _, err := command.Parse(args, &help); err == nil {
			t.Fatal("invalid invocation accepted", args)
		}
	}
	if err := (command.Command{}).Run(t.Context(), nil, &help); !errors.Is(err, fault.Invalid) {
		t.Fatal("unparsed command ran", err)
	}
	if _, err := command.Declaration(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("declaration accepted a missing store constructor", err)
	}
}

// rotating stores factor envelopes in memory, ordered by owner.
type rotating struct {
	mfa.Backend
	factors []mfa.StoredCiphertext
}

func (b *rotating) StaleCiphertexts(_ context.Context, active encryption.KeyID, after string, limit int) ([]mfa.StoredCiphertext, error) {
	var result []mfa.StoredCiphertext
	for _, item := range b.factors {
		if item.Owner > after && item.Ciphertext.KeyID() != active && len(result) < limit {
			result = append(result, item)
		}
	}
	return result, nil
}
func (b *rotating) ReplaceCiphertext(_ context.Context, stored mfa.StoredCiphertext, next encryption.Ciphertext) (bool, error) {
	for i, item := range b.factors {
		if item.Owner == stored.Owner && item.Generation == stored.Generation && item.Ciphertext.Encoded() == stored.Ciphertext.Encoded() {
			b.factors[i].Ciphertext = next
			return true, nil
		}
	}
	return false, nil
}

// The command drains every batch, reports committed counts without factor
// material, and fails after reporting when a retired key is missing.
func TestMFAReencryptRunsBatchesAndReportsFailures(t *testing.T) {
	old, err := encryption.GenerateKey("mfa_old")
	if err != nil {
		t.Fatal(err)
	}
	next, err := encryption.GenerateKey("mfa_new")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := encryption.GenerateKey("mfa_gone")
	if err != nil {
		t.Fatal(err)
	}
	backend := &rotating{}
	for i, keys := range []encryption.Key{old, old, old, gone} {
		ring, err := encryption.NewKeyring(keys.ID(), keys)
		if err != nil {
			t.Fatal(err)
		}
		generation, _ := model.NewID[mfa.Record]()
		owner := fmt.Sprintf("%064x", i+1)
		binding, _ := encryption.NewContext("auth.mfa.totp.v1", secret.New(owner+":"+generation.String()))
		ciphertext, err := ring.Encrypt(t.Context(), binding, secret.New("JBSWY3DPEHPK3PXP"))
		if err != nil {
			t.Fatal(err)
		}
		backend.factors = append(backend.factors, mfa.StoredCiphertext{Owner: owner, Generation: generation, Ciphertext: ciphertext})
	}
	keys, err := encryption.NewKeyring("mfa_new", old, next)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mfa.NewStore(backend, keys, mfa.DefaultConfig(keyspace.Namespace{Application: "mfa-command", Environment: "test"}, "Command"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := command.Parse([]string{"mfa", "reencrypt", "--batch", "1", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := parsed.Run(t.Context(), store, &output); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing retired key did not fail the command", err)
	}
	var report struct {
		Reencrypted, Changed, Failed uint64
		Complete                     bool
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Reencrypted != 3 || report.Failed != 1 || !report.Complete {
		t.Fatal("unexpected report", output.String(), err)
	}
	if strings.Contains(output.String(), "JBSWY3DPEHPK3PXP") {
		t.Fatal("report disclosed factor material")
	}
	for _, item := range backend.factors[:3] {
		if item.Ciphertext.KeyID() != "mfa_new" {
			t.Fatal("factor was not rotated")
		}
	}
}
