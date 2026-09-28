package cache

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxTags bounds application tag references per operation. One additional reserved
// namespace stamp is permitted. Config.MaxTags may impose a smaller application limit.
const MaxTags = 64
const TagVersionBytes = 16

// TagVersion is an opaque random identity. Missing metadata must get a fresh
// version; recreating a previous/default version could resurrect stale values.
type TagVersion struct{ value [TagVersionBytes]byte }

func NewTagVersion() (TagVersion, error) {
	var v TagVersion
	if _, err := rand.Read(v.value[:]); err != nil {
		return TagVersion{}, fault.Wrap(fault.Internal, "cache tag entropy failed", err)
	}
	if err := v.Validate(); err != nil {
		return TagVersion{}, err
	}
	return v, nil
}
func ParseTagVersion(data []byte) (TagVersion, error) {
	if len(data) != TagVersionBytes {
		return TagVersion{}, fault.New(fault.Invalid, "invalid cache tag version size")
	}
	var v TagVersion
	copy(v.value[:], data)
	return v, v.Validate()
}
func (v TagVersion) Validate() error {
	if v.value == [TagVersionBytes]byte{} {
		return fault.New(fault.Invalid, "cache tag version is not initialized")
	}
	return nil
}
func (v TagVersion) Bytes() []byte { return slices.Clone(v.value[:]) }

// TagStamp is the adapter boundary for a resolved tag identity and version.
type TagStamp struct {
	Key     EntryKey
	Version TagVersion
}
type taggedKeyData struct {
	key         EntryKey
	flight      EntryKey
	stamps      []TagStamp
	fingerprint [sha256.Size]byte
}

// TaggedKey owns an immutable canonical snapshot. Its data address is stable
// across invalidations; its fill address includes versions for Remember isolation.
type TaggedKey struct{ data *taggedKeyData }

// NewTaggedKey is for adapters. Applications construct typed tags and WithTags
// views. Duplicate keys with conflicting versions and cross-namespace tags fail.
func NewTaggedKey(base EntryKey, stamps []TagStamp) (TaggedKey, error) {
	if err := base.Validate(); err != nil {
		return TaggedKey{}, err
	}
	if base.namespaceTag {
		return TaggedKey{}, fault.New(fault.Invalid, "namespace metadata cannot be a cache payload address")
	}

	if len(stamps) == 0 || len(stamps) > MaxTags+1 {
		return TaggedKey{}, fault.New(fault.Invalid, "invalid cache tag count")
	}
	ordered := slices.Clone(stamps)
	applicationTags := 0
	for _, stamp := range ordered {
		if !stamp.Key.namespaceTag {
			applicationTags++
		}
		if err := stamp.Key.Validate(); err != nil {
			return TaggedKey{}, err
		}
		if err := stamp.Version.Validate(); err != nil {
			return TaggedKey{}, err
		}
		if stamp.Key.Namespace() != base.Namespace() {
			return TaggedKey{}, fault.New(fault.Invalid, "cache tags cross namespaces")
		}
	}
	if applicationTags > MaxTags {
		return TaggedKey{}, fault.New(fault.Invalid, "invalid cache tag count")
	}
	slices.SortFunc(ordered, func(a, b TagStamp) int { return strings.Compare(a.Key.String(), b.Key.String()) })
	unique := ordered[:0]
	for _, stamp := range ordered {
		if len(unique) > 0 && unique[len(unique)-1].Key == stamp.Key {
			if unique[len(unique)-1].Version != stamp.Version {
				return TaggedKey{}, fault.New(fault.Invalid, "cache tag versions conflict")
			}
			continue
		}
		unique = append(unique, stamp)
	}
	identity := sha256.New()
	versions := sha256.New()
	identity.Write([]byte("tagged\x00"))
	identity.Write(base.address.Hash[:])
	versions.Write([]byte("versions\x00"))
	for _, stamp := range unique {
		text := []byte(stamp.Key.String())
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(text)))
		identity.Write(size[:])
		identity.Write(text)
		versions.Write(size[:])
		versions.Write(text)
		versions.Write(stamp.Version.value[:])
	}
	data := &taggedKeyData{key: base, flight: base, stamps: slices.Clone(unique)}
	copy(data.key.address.Hash[:], identity.Sum(nil))
	copy(data.fingerprint[:], versions.Sum(nil))
	fillBytes := append([]byte("tagged-fill\x00"), data.key.address.Hash[:]...)
	fillBytes = append(fillBytes, data.fingerprint[:]...)
	data.flight.address.Hash = sha256.Sum256(fillBytes)
	return TaggedKey{data: data}, nil
}
func (k TaggedKey) Validate() error {
	if k.data == nil {
		return fault.New(fault.Invalid, "tagged cache key is not initialized")
	}
	return nil
}
func (k TaggedKey) DataKey() EntryKey {
	if k.data == nil {
		return EntryKey{}
	}
	return k.data.key
}
func (k TaggedKey) FillKey() EntryKey {
	if k.data == nil {
		return EntryKey{}
	}
	return k.data.flight
}
func (k TaggedKey) Stamps() []TagStamp {
	if k.data == nil {
		return nil
	}
	return slices.Clone(k.data.stamps)
}
func (k TaggedKey) Fingerprint() [sha256.Size]byte {
	if k.data == nil {
		return [sha256.Size]byte{}
	}
	return k.data.fingerprint
}
