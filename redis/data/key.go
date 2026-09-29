package data

import (
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Name string
type Version uint32
type Kind string

const (
	HashKind      Kind = "hash"
	SetKind       Kind = "set"
	SortedSetKind Kind = "zset"
	ListKind      Kind = "list"
)

// Key is an opaque adapter address, separate from cache and other feature keys.
// Application calls preserve their concrete resource types through Hash, Set,
// SortedSet or List. The kind is part of the address and matches Redis TYPE.
type Key struct {
	address keyaddress.Address
	version Version
	kind    Kind
}

func NewKey(namespace keyspace.Namespace, name Name, version Version, kind Kind, logical string) (Key, error) {
	a, err := keyaddress.New(namespace, string(name), logical)
	if err != nil {
		return Key{}, err
	}
	k := Key{a, version, kind}
	return k, k.Validate()
}
func (k Key) Validate() error {
	if k.version == 0 || k.kind != HashKind && k.kind != SetKind && k.kind != SortedSetKind && k.kind != ListKind {
		return fault.New(fault.Invalid, "invalid Redis data key kind or version")
	}
	return k.address.Validate()
}
func (k Key) String() string {
	if k.Validate() != nil {
		return ""
	}
	return k.address.String("data-"+string(k.kind)) + ":" + strconv.FormatUint(uint64(k.version), 10)
}
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }
func (k Key) Kind() Kind                    { return k.kind }

// ValidateBatch requires a canonical, bounded batch within one namespace.
func ValidateBatch(keys []Key) error {
	if len(keys) > MaxBatchKeys {
		return fault.New(fault.Invalid, "Redis data batch exceeds its bound")
	}
	for i, k := range keys {
		if err := k.Validate(); err != nil {
			return err
		}
		if i > 0 && (k.Namespace() != keys[0].Namespace() || k.String() <= keys[i-1].String()) {
			return fault.New(fault.Invalid, "Redis data batch must be unique, ordered and in one namespace")
		}
	}
	return nil
}
