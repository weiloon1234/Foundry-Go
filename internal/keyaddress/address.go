// Package keyaddress owns bounded physical address encoding for feature adapters.
package keyaddress

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Address struct {
	Namespace keyspace.Namespace
	Name      string
	Hash      [sha256.Size]byte
	valid     bool
}

func New(namespace keyspace.Namespace, name, logical string) (Address, error) {
	if err := namespace.Validate(); err != nil {
		return Address{}, err
	}
	if !keyspace.ValidName(name) || len(logical) == 0 || len(logical) > keyspace.MaxKeyBytes || !utf8.ValidString(logical) {
		return Address{}, fault.New(fault.Invalid, "invalid family or logical key")
	}
	for _, r := range logical {
		if unicode.IsControl(r) {
			return Address{}, fault.New(fault.Invalid, "keys cannot contain control characters")
		}
	}
	return Address{Namespace: keyspace.Namespace{Application: strings.Clone(namespace.Application), Environment: strings.Clone(namespace.Environment)}, Name: strings.Clone(name), Hash: sha256.Sum256([]byte(logical)), valid: true}, nil
}
func (a Address) Validate() error {
	if !a.valid {
		return fault.New(fault.Invalid, "key is not initialized")
	}
	return nil
}
func (a Address) String(feature string) string {
	if !a.valid {
		return ""
	}
	return "foundry:" + feature + ":v1:" + a.Namespace.Application + ":" + a.Namespace.Environment + ":" + a.Name + ":" + hex.EncodeToString(a.Hash[:])
}
